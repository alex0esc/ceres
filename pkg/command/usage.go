package command

import (
	"fmt"
	"strings"
)

// NoArgs is the uniform error message for a command that takes no arguments but
// was given some. path is the full command path without the leading slash
// (e.g. "clear" or "wakeup list").
func NoArgs(path string) string {
	return fmt.Sprintf("The /%s command does not take any arguments.\nUsage: /%s", path, path)
}

// NeedArgs is the uniform error message for a command that requires arguments
// but was called without them.
func NeedArgs(path, argUsage string) string {
	return fmt.Sprintf("The /%s command requires %s.\nUsage: /%s %s", path, argUsage, path, argUsage)
}

// SubcommandUsage is the uniform error message shown when a command that has
// subcommands is called without one, or with an unknown one. It lists the
// available subcommands so the command system is navigable without autocomplete.
func SubcommandUsage(path string, subs []Command) string {
	return fmt.Sprintf("The /%s command requires a subcommand.\nAvailable subcommands: %s",
		path, subcommandNames(subs))
}

// subcommandNames returns the subcommand names joined for display.
func subcommandNames(subs []Command) string {
	names := make([]string, 0, len(subs))
	for _, s := range subs {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}
