package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)

const wakeUsage = "Usage: `/wake list` or `/wake run <name>`"

func NewWakeupCommand() command.Command {
	return command.Command{
		Name:        "wake",
		Description: "Manage registered wakeups for the specific agent.",
		Handler:     handleWakeup,
	}
}

func handleWakeup(agent handles.AgentHandle, args []string) string {
	if len(args) == 0 {
		return wakeUsage
	}

	mgr := agent.WakeupManager()

	switch strings.ToLower(args[0]) {
	case "list":
		return wakeList(mgr)
	case "run":
		return wakeRun(mgr, args[1:])
	default:
		return "Unknown subcommand. " + wakeUsage
	}
}

func wakeList(mgr *wakeup.Manager) string {
	internalWakeups := mgr.ListInternalWakeups()
	externalWakeups := mgr.ListExternalWakeups()

	if len(internalWakeups) == 0 && len(externalWakeups) == 0 {
		return "No wakeups registered."
	}

	var b strings.Builder
	b.WriteString("## Registered Wakeups\n\n")

	if len(internalWakeups) > 0 {
		b.WriteString("### Internal Wakeups\n\n")
		b.WriteString("Wakeups created by the agent. These can be edited or removed by the agent.\n\n")

		for _, wu := range internalWakeups {
			fmt.Fprintf(&b, "#### %s\n", wu.Name())
			fmt.Fprintf(&b, "**Description:** %s\n", wu.Description())

			if fireAt := wu.FireAt(); !fireAt.IsZero() {
				fmt.Fprintf(&b, "**Runs at:** %s\n", fireAt.Format(time.RFC3339))
			} else if cronSpec := strings.TrimSpace(wu.CronSpec()); cronSpec != "" {
				fmt.Fprintf(&b, "**Schedule:** `%s`\n", cronSpec)
			}

			b.WriteString("\n")
		}
	}

	if len(externalWakeups) > 0 {
		b.WriteString("### External Wakeups\n\n")
		b.WriteString("Wakeups created by the user. These are fixed and cannot be edited or removed by the agent.\n\n")

		for _, wu := range externalWakeups {
			fmt.Fprintf(&b, "#### %s\n", wu.Name())
			fmt.Fprintf(&b, "**Description:** %s\n", wu.Description())

			if fireAt := wu.FireAt(); !fireAt.IsZero() {
				fmt.Fprintf(&b, "**Runs at:** %s\n", fireAt.Format(time.RFC3339))
			} else if cronSpec := strings.TrimSpace(wu.CronSpec()); cronSpec != "" {
				fmt.Fprintf(&b, "**Schedule:** `%s`\n", cronSpec)
			}

			b.WriteString("\n")
		}
	}

	return b.String()
}

func wakeRun(mgr *wakeup.Manager, args []string) string {
	if len(args) < 1 {
		return "Please specify a wakeup name. Usage: `/wake run <name>`"
	}

	name := args[0]

	if !mgr.Run(name) {
		return fmt.Sprintf("Wakeup with name '%s' does not exist.", name)
	}

	return fmt.Sprintf("*Queued wakeup '%s' for execution.*", name)
}


