package discord

import (
	"fmt"
	"log"
	"strings"

	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/bwmarrin/discordgo"
)

// handleInteraction processes slash-command invocations and their option
// autocomplete. Only the configured user is allowed, mirroring the DM handler.
func (d *Discord) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := interactionUserID(i.Interaction)
	if d.userID == "" || userID != d.userID {
		return
	}

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		if data.Name == "agent" {
			d.handleAgentCommand(s, i, data)
			return
		}

		agent, ok := d.currentAgent()
		if !ok {
			return
		}
		text := interactionCommandText(data)
		if handled, msg := command.CheckCommand(agent, text); handled {
			d.respondCommand(s, i, msg)
		}

	case discordgo.InteractionApplicationCommandAutocomplete:
		data := i.ApplicationCommandData()
		var choices []*discordgo.ApplicationCommandOptionChoice
		if data.Name == "agent" {
			choices = d.agentNameChoices(focusedOptionValue(data))
		} else {
			agent, ok := d.currentAgent()
			if !ok {
				return
			}
			choices = autocompleteChoices(data, agent)
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{Choices: choices},
		})
	}
}

// handleAgentCommand handles the platform-local /agent command.
func (d *Discord) handleAgentCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if len(data.Options) == 0 {
		d.respondCommand(s, i, agentUsage("missing subcommand"))
		return
	}

	sub := data.Options[0]
	var msg string
	switch sub.Name {
	case "list":
		msg = d.agentListMessage()
	case "choose":
		if len(sub.Options) == 0 {
			msg = agentUsage("missing agent name")
		} else {
			msg = d.chooseAgent(stringOptionValue(sub.Options[0]))
		}
	default:
		msg = agentUsage(fmt.Sprintf("unknown subcommand %q", sub.Name))
	}

	d.respondCommand(s, i, msg)
}

// agentUsage builds a helpful error for malformed /agent invocations.
func agentUsage(detail string) string {
	if detail == "" {
		return "Usage: /agent list | /agent choose <name>"
	}
	return fmt.Sprintf("Invalid /agent command: %s.\nUsage: /agent list | /agent choose <name>", detail)
}

// respondCommand answers a slash-command interaction. The interaction is first
// acknowledged (required within 3s), the resulting "thinking" placeholder is
// immediately deleted, and the actual result is always sent as regular channel
// messages via SendChunked. This way the placeholder is never visible.
func (d *Discord) respondCommand(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("discord: failed to acknowledge interaction: %v", err)
		return
	}
	if err := s.InteractionResponseDelete(i.Interaction); err != nil {
		log.Printf("discord: failed to clear interaction placeholder: %v", err)
	}
	tools.SendChunked(s, i.ChannelID, msg)
}

// interactionUserID returns the id of the user who triggered the interaction,
// which is filled in differently for DMs (User) and guilds (Member.User).
func interactionUserID(i *discordgo.Interaction) string {
	if i.User != nil {
		return i.User.ID
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	return ""
}

// autocompleteChoices returns the wakeup names matching the currently focused
// option, so /wakeup run offers them as choices.
func autocompleteChoices(data discordgo.ApplicationCommandInteractionData, agent handles.AgentHandle) []*discordgo.ApplicationCommandOptionChoice {
	current := strings.ToLower(focusedOptionValue(data))

	mgr := agent.WakeupManager()
	if mgr == nil {
		return nil
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	add := func(name string) {
		if len(choices) >= 25 {
			return
		}
		if current == "" || strings.Contains(strings.ToLower(name), current) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: name})
		}
	}
	for _, w := range mgr.ListInternalWakeups() {
		add(w.Name())
	}
	for _, w := range mgr.ListExternalWakeups() {
		add(w.Name())
	}
	return choices
}

// focusedOptionValue returns the value of the option the user is currently
// typing, searching through subcommand nesting.
func focusedOptionValue(data discordgo.ApplicationCommandInteractionData) string {
	for _, opt := range data.Options {
		if opt.Focused {
			return stringOptionValue(opt)
		}
		for _, sub := range opt.Options {
			if sub.Focused {
				return stringOptionValue(sub)
			}
		}
	}
	return ""
}
