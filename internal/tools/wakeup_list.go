
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WakeupListTool lets an agent list its own scheduled wakeups (including
// user-created ones which it cannot edit or remove).
type WakeupListTool struct{}

// NewWakeupListTool constructs a WakeupListTool.
func NewWakeupListTool() WakeupListTool {
	return WakeupListTool{}
}

func (WakeupListTool) Name() string {
	return "wakeup_list"
}

func (WakeupListTool) Description() string {
	return "List all currently scheduled wakeups for this agent. " +
		"Returns internal wakeups (created by you, can be edited/removed) and external wakeups (created by user, read-only). " +
		"Each wakeup shows its name, description, schedule (fire_at or cron_spec), and whether it can be deleted. " +
		"Always use this tool before adding or removing wakeups to see what exists and avoid name conflicts."
}

func (WakeupListTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t WakeupListTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		if handle == nil {
			return "", fmt.Errorf("wakeup_list: agent handle is nil")
		}

		internalWakeups := handle.WakeupManager().ListInternalWakeups()
		externalWakeups := handle.WakeupManager().ListExternalWakeups()

		type entry struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			FireAt      string `json:"fire_at,omitempty"`
			CronSpec    string `json:"cron_spec,omitempty"`
			CanDelete   bool   `json:"can_delete"`
		}

		internal := make([]entry, 0, len(internalWakeups))
		for _, w := range internalWakeups {
			if w == nil {
				continue
			}

			e := entry{
				Name:        w.Name(),
				Description: w.Description(),
				CronSpec:    w.CronSpec(),
				CanDelete:   true,
			}

			if fireAt := w.FireAt(); !fireAt.IsZero() {
				e.FireAt = fireAt.Format(time.RFC3339)
			}

			internal = append(internal, e)
		}

		external := make([]entry, 0, len(externalWakeups))
		for _, w := range externalWakeups {
			if w == nil {
				continue
			}

			e := entry{
				Name:        w.Name(),
				Description: w.Description(),
				CronSpec:    w.CronSpec(),
				CanDelete:   false,
			}

			if fireAt := w.FireAt(); !fireAt.IsZero() {
				e.FireAt = fireAt.Format(time.RFC3339)
			}

			external = append(external, e)
		}

		result := struct {
			Internal []entry `json:"internal"`
			External []entry `json:"external"`
		}{
			Internal: internal,
			External: external,
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("wakeup_list: failed to marshal result: %w", err)
		}

		return string(out), nil
	}
}
