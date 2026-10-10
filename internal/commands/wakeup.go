package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)

// NewWakeupCommand returns the /wakeup command together with its subcommands.
// Registering this single command registers the whole tree; the subcommands are
// embedded and are not separate top-level commands.
func NewWakeupCommand() command.Command {
	return command.Command{
		Name:        "wakeup",
		Description: "Manage registered wakeups for the specific agent.",
		Subcommands: []command.Command{
			{
				Name:        "list",
				Description: "List all registered wakeups.",
				Handler:     handleWakeupList,
			},
			{
				Name:        "run",
				Description: "Run a registered wakeup by name.",
				Handler:     handleWakeupRun,
			},
		},
	}
}

func handleWakeupList(agent handles.AgentHandle, args []string) string {
	if len(args) > 0 {
		return command.NoArgs("wakeup list")
	}
	return wakeList(agent.WakeupManager())
}

func handleWakeupRun(agent handles.AgentHandle, args []string) string {
	if len(args) < 1 {
		return command.NeedArgs("wakeup run", "<name>")
	}

	name := args[0]
	mgr := agent.WakeupManager()

	if !mgr.Run(name) {
		return fmt.Sprintf("No wakeup named '%s' is registered.\nTip: run /wakeup list to see all wakeups.", name)
	}

	return fmt.Sprintf("*Queued wakeup '%s' for execution.*", name)
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


