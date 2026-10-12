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

// Command is a single slash command. A command may carry Subcommands, which are
// themselves Commands. A command that has Subcommands is only a grouping node:
// it is never runnable on its own and must be invoked together with one of its
// subcommands. A command without Subcommands is a leaf and runs its Handler,
// receiving the remaining tokens as free-form arguments.
type Command struct {
	Name        string
	Description string
	Handler     CommandHandler
	Subcommands []Command
}

// Commands is the global registry, keyed by name (without "/").
var registry = map[string]Command{}


// Register adds a command to the global registry. A command that has subcommands
// but no own Handler gets an automatically generated Handler that dispatches to
// the matching subcommand, so callers (TUI, platforms) only ever deal with the
// top-level command's Handler and never need to know about the subcommand tree.
func Register(cmd Command) {
	_, ok := registry[cmd.Name]
	if ok {
		log.Fatalf("command %s already registered", cmd.Name)
	}
	if cmd.Handler == nil && len(cmd.Subcommands) > 0 {
		cmd.Handler = subcommandHandler(cmd)
	}
	registry[cmd.Name] = cmd
}

// subcommandHandler builds a Handler for a parent command that resolves the
// first argument to one of its subcommands and runs that subcommand's Handler
// with the remaining arguments. Wrong or missing subcommands return the uniform
// SubcommandUsage message.
func subcommandHandler(parent Command) CommandHandler {
	usage := SubcommandUsage(parent.Name, parent.Subcommands)
	return func(agent handles.AgentHandle, args []string) string {
		if len(args) == 0 {
			return usage
		}
		child, ok := parent.Subcommand(args[0])
		if !ok {
			return usage
		}
		if child.Handler == nil {
			return fmt.Sprintf("The /%s %s subcommand cannot be run directly.\n%s", parent.Name, child.Name, usage)
		}
		return child.Handler(agent, args[1:])
	}
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



// Lookup returns the top-level command with the given name (case-insensitive).
func Lookup(name string) (Command, bool) {
	c, ok := registry[strings.ToLower(name)]
	return c, ok
}

// Subcommand returns the direct subcommand of c with the given name
// (case-insensitive).
func (c Command) Subcommand(name string) (Command, bool) {
	name = strings.ToLower(name)
	for _, s := range c.Subcommands {
		if strings.ToLower(s.Name) == name {
			return s, true
		}
	}
	return Command{}, false
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
		return true, fmt.Sprintf("Invalid command: %v", err)
	}
	if len(fields) == 0 {
		// only "/" was entered without a command name
		return false, ""
	}
	name := strings.ToLower(fields[0])
	args := fields[1:]

	cmd, ok := registry[name]
	if !ok {
		return true, fmt.Sprintf("Unknown command: /%s.\nRun /help to see all available commands.", name)
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
