package commands

import (
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)


func init() {
	command.Register(command.Command{
		Name:        "help",
		Description: "Show this help message.",
		Handler:     handleHelp,
	})
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
