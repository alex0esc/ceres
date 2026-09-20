
package command

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode"

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


func ClearRegistry() {
	registry = make(map[string]Command)
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
	// remove the leading "/" and split into fields by whitespace,
	// text in double quotes counts as a single field
	trimmed := strings.TrimPrefix(cmdText, "/")
	fields, err := splitArgs(trimmed)
	if err != nil {
		return true, fmt.Sprintf("Invalid command: %v\n", err)
	}
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


// splitArgs splits s into arguments at whitespace. An argument in double
// quotes (straight or typographic, as mobile keyboards produce them) is taken
// exactly as typed, including any whitespace, and always counts as a single
// argument; only the surrounding quotes are removed. "" yields an empty
// argument. Quotes are only allowed around a whole argument and cannot occur
// inside a quoted string, anything else is an error.
func splitArgs(s string) ([]string, error) {
	var args []string
	runes := []rune(s)
	i := 0

	for i < len(runes) {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}

		if isQuote(runes[i]) {
			// quoted argument: everything up to the next quote, unchanged
			end := i + 1
			for end < len(runes) && !isQuote(runes[end]) {
				end++
			}
			if end == len(runes) {
				return nil, errors.New("unterminated quote")
			}
			args = append(args, string(runes[i+1:end]))
			i = end + 1
			if i < len(runes) && !unicode.IsSpace(runes[i]) {
				return nil, errors.New("a closing quote must be followed by a space")
			}
			continue
		}

		// plain argument: up to the next whitespace, must not contain quotes
		end := i
		for end < len(runes) && !unicode.IsSpace(runes[end]) {
			if isQuote(runes[end]) {
				return nil, errors.New("quotes are only allowed around a whole argument")
			}
			end++
		}
		args = append(args, string(runes[i:end]))
		i = end
	}

	return args, nil
}

func isQuote(r rune) bool {
	return r == '"' || r == '“' || r == '”'
}
