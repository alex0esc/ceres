package command

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/alex0esc/ceres/pkg/handles"
)

// CommandHandler runs a command and returns the response text.
type CommandHandler func(agent handles.AgentHandle, args []string) string

// Command is a single slash command.
type Command struct {
	Name        string
	Description string
	Handler     CommandHandler
}

// Commands is the global registry, keyed by name (without "/").
var registry = map[string]Command{}


// RegisterCommand adds a command to the global registry.
func Register(cmd Command) {
	_, ok := registry[cmd.Name]
	if ok {
		log.Fatalf("command %s already registered", cmd.Name)
	}
	registry[cmd.Name] = cmd
}


// All returns all registered commands, sorted alphabetically by name.
func All() []Command {
	out := make([]Command, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}



// CheckCommand parses cmdText and, if it's a command, runs it.
// Returns true if the text was handled as a command.
func CheckCommand(agent handles.AgentHandle, cmdText string) (bool, string) {
	cmdText = strings.TrimSpace(cmdText)
	if !strings.HasPrefix(cmdText, "/") {
		return false, ""
	}
	// remove the leading "/" and split into fields by whitespace
	trimmed := strings.TrimPrefix(cmdText, "/")
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		// only "/" was entered without a command name
		return false, ""
	}
	name := strings.ToLower(fields[0])
	args := fields[1:]

	cmd, ok := registry[name]
	if !ok {
		return true, fmt.Sprintf("Unknown command: /%s\n", name)
	}
	return true, cmd.Handler(agent, args)
}
