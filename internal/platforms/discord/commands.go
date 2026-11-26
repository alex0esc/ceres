package discord

import (
	"log"
	"strings"

	"github.com/alex0esc/ceres/pkg/command"
	"github.com/bwmarrin/discordgo"
)

// commandArgs declares the argument options for commands/subcommands that take
// free-form arguments. The key is the command path without a leading slash
// ("wakeup run"). Commands not listed here take no arguments. This mapping is
// Discord-specific and kept out of the command model on purpose.
var commandArgs = map[string][]*discordgo.ApplicationCommandOption{
	"wakeup run": {
		{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         "name",
			Description:  "Name of the wakeup to run.",
			Required:     true,
			Autocomplete: true,
		},
	},
}

// registerCommands registers all commands (and their subcommands) as global
// Discord application commands, so they show up in the native slash-command
// picker with their descriptions. Global commands are also available in DMs
// with the bot (given a mutual guild).
func (d *Discord) registerCommands(s *discordgo.Session) {
	if s.State.User == nil {
		return
	}
	appID := s.State.User.ID
	if _, err := s.ApplicationCommandBulkOverwrite(appID, "", buildApplicationCommands()); err != nil {
		log.Printf("discord: failed to register application commands: %v", err)
	}
}

// buildApplicationCommands converts the registered command tree into Discord
// application commands: subcommands become SUB_COMMAND options, and any
// declared argument options are attached to the matching subcommand.
func buildApplicationCommands() []*discordgo.ApplicationCommand {
	cmds := make([]*discordgo.ApplicationCommand, 0, 8)
	for _, c := range command.All() {
		ac := &discordgo.ApplicationCommand{
			Name:        c.Name,
			Description: c.Description,
		}
		for _, sub := range c.Subcommands {
			opt := &discordgo.ApplicationCommandOption{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        sub.Name,
				Description: sub.Description,
			}
			opt.Options = commandArgs[c.Name+" "+sub.Name]
			ac.Options = append(ac.Options, opt)
		}
		// top-level argument options (none currently, but supported)
		ac.Options = append(ac.Options, commandArgs[c.Name]...)
		cmds = append(cmds, ac)
	}

	cmds = append(cmds, agentApplicationCommand())
	return cmds
}

// agentApplicationCommand is platform-local and therefore not part of the
// global command registry.
func agentApplicationCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "agent",
		Description: "List or choose the agent used in this chat.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "list",
				Description: "List all agents and mark the current one.",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "choose",
				Description: "Choose the agent for this chat.",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:         discordgo.ApplicationCommandOptionString,
						Name:         "name",
						Description:  "Agent name.",
						Required:     true,
						Autocomplete: true,
					},
				},
			},
		},
	}
}

// interactionCommandText rebuilds the textual slash command from an interaction
// so it can be fed straight into command.CheckCommand, keeping behavior and
// error messages identical to the TUI and plain-text path.
func interactionCommandText(data discordgo.ApplicationCommandInteractionData) string {
	parts := []string{"/" + data.Name}
	for _, opt := range data.Options {
		if opt.Type == discordgo.ApplicationCommandOptionSubCommand {
			parts = append(parts, opt.Name)
			for _, arg := range opt.Options {
				parts = append(parts, quoteArg(stringOptionValue(arg)))
			}
			continue
		}
		parts = append(parts, quoteArg(stringOptionValue(opt)))
	}
	return strings.Join(parts, " ")
}

// stringOptionValue extracts a string option value, returning "" for others.
func stringOptionValue(opt *discordgo.ApplicationCommandInteractionDataOption) string {
	if s, ok := opt.Value.(string); ok {
		return s
	}
	return ""
}

// quoteArg wraps a value in double quotes when it contains whitespace, so it
// survives the command argument splitter as a single argument.
func quoteArg(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, " \t\n\"") {
		return `"` + strings.ReplaceAll(v, `"`, "") + `"`
	}
	return v
}
