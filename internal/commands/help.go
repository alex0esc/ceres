package commands

import (
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)


func NewHelpCommand() command.Command {
	return command.Command{
		Name:        "help",
		Description: "Show a help message how to use other commands.",
		Handler:     handleHelp,
	}
}


func handleHelp(agent handles.AgentHandle, args []string) string {
	if len(args) > 0 {
		return "The help command does not take arguments!"
	}


	var b strings.Builder
	b.WriteString("## Available Commands\n\n")
	for _, cmd := range command.All() {
		fmt.Fprintf(&b, "- **/%s** — %s\n", cmd.Name, cmd.Description)
	}

	return b.String()
}
