package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)

func NewWakeupCommand() command.Command {
	return command.Command{
		Name:        "wake",
		Description: "Manage registered wakeups for the specific agent.",
		Handler: func(agent handles.AgentHandle, args []string) string {
			return handleWakeup(agent, args)
		},
	}
}

func handleWakeup(agent handles.AgentHandle, args []string) string {
	if len(args) == 0 {
		return "Usage: `/wake list` or `/wake run <name>`"
	}

	subcommand := strings.ToLower(args[0])

	switch subcommand {
	case "list":
		wakeups := agent.ListWakeups()

		if len(wakeups) == 0 {
			return "No wakeups registered."
		}

		var b strings.Builder
		b.WriteString("## Registered Wakeups\n\n")

		for _, wakeup := range wakeups {
			fmt.Fprintf(&b, "#### %s\n", wakeup.Name())
			fmt.Fprintf(&b, "**Description:** %s\n", wakeup.Description())

			if fireAt := wakeup.FireAt(); fireAt != nil {
				fmt.Fprintf(&b, "**Runs at:** %s\n", fireAt.Format(time.RFC3339))
			} else if cronSpec := strings.TrimSpace(wakeup.CroneSpec()); cronSpec != "" {
				fmt.Fprintf(&b, "**Schedule:** `%s`\n", cronSpec)
			}

			fmt.Fprintf(&b, "**Protected:** %t\n\n", wakeup.Protected())
		}

		return b.String()

	case "run":
		if len(args) < 2 {
			return "Please specify a wakeup name. Usage: `/wake run <name>`"
		}

		name := args[1]

		if !agent.ExecuteWakeup(name) {
			return fmt.Sprintf("Wakeup with name '%s' does not exist.", name)
		}

		return fmt.Sprintf("*Queued wakeup '%s' for execution.*", name)

	default:
		return "Unknown subcommand. Usage: `/wake list` or `/wake run <name>`"
	}
}
