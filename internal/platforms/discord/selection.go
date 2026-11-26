package discord

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/bwmarrin/discordgo"
)

// currentAgent resolves the currently selected agent. An empty or stale
// selection (e.g. after an agent was removed) falls back to the configured
// default agent.
func (d *Discord) currentAgent() (handles.AgentHandle, bool) {
	d.selectedMutex.Lock()
	name := d.selectedAgentName
	d.selectedMutex.Unlock()
	if name == "" {
		name = d.agentName
	}

	agnt, ok := d.agents[name]
	if !ok {
		agnt, ok = d.agents[d.agentName]
	}
	return agnt, ok
}

// chooseAgent stores the selection and returns the response text.
func (d *Discord) chooseAgent(name string) string {
	if _, ok := d.agents[name]; !ok {
		return fmt.Sprintf("Unknown agent %q. Use /agent list to see available agents.", name)
	}

	d.selectedMutex.Lock()
	d.selectedAgentName = name
	d.selectedMutex.Unlock()
	return fmt.Sprintf("Agent set to **%s**.", name)
}

// agentListMessage lists all agents, marking the one currently selected.
func (d *Discord) agentListMessage() string {
	current, _ := d.currentAgent()

	var b strings.Builder
	b.WriteString("Available agents:\n\n")
	for _, name := range slices.Sorted(maps.Keys(d.agents)) {
		displayName := name
		if current != nil && current.Name() == name {
			displayName = "**" + name + "**"
		}

		description := d.agents[name].Description()
		if description == "" {
			b.WriteString("- " + displayName + "\n")
		} else {
			b.WriteString("- " + displayName + ": " + description + "\n")
		}
	}
	return b.String()
}

// agentNameChoices provides autocomplete choices for /agent choose.
func (d *Discord) agentNameChoices(current string) []*discordgo.ApplicationCommandOptionChoice {
	current = strings.ToLower(current)
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	for _, name := range slices.Sorted(maps.Keys(d.agents)) {
		if len(choices) >= 25 {
			break
		}
		if current == "" || strings.Contains(strings.ToLower(name), current) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: name})
		}
	}
	return choices
}
