package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WakeupInspectTool lets an agent inspect the details of a specific wakeup.
type WakeupInspectTool struct{}

// NewWakeupInspectTool constructs a WakeupInspectTool.
func NewWakeupInspectTool() WakeupInspectTool {
	return WakeupInspectTool{}
}

func (WakeupInspectTool) Name() string {
	return "wakeup_inspect"
}

func (WakeupInspectTool) Description() string {
	return "Inspect a specific wakeup by name to see its full details including description, schedule, prompts, timeout, and status. " +
		"Use wakeup_list first to see which wakeups exist, then use this tool to examine a specific wakeup in detail. " +
		"Returns all information about the wakeup including the complete prompts that will be sent when it fires."
}

func (WakeupInspectTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the wakeup to inspect",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func (WakeupInspectTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		if handle == nil {
			return "", fmt.Errorf("wakeup_inspect: agent handle is nil")
		}

		var args struct {
			Name string `json:"name"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("wakeup_inspect: invalid arguments: %w", err)
		}

		name := strings.TrimSpace(args.Name)
		if name == "" {
			return "", fmt.Errorf("wakeup_inspect: name is required")
		}

		wakeup, ok := handle.WakeupManager().Get(name)
		if !ok {
			return "", fmt.Errorf("wakeup_inspect: no wakeup named %q", name)
		}

		// Determine if it's internal or external
		internalWakeups := handle.WakeupManager().ListInternalWakeups()
		isInternal := false
		for _, w := range internalWakeups {
			if w.Name() == name {
				isInternal = true
				break
			}
		}

		// Extract prompts from the task
		prompts := make([]string, 0)
		for _, prompt := range wakeup.Task().Prompts {
			prompts = append(prompts, prompt.Text)
		}

		// Build the result
		result := struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			FireAt      string   `json:"fire_at,omitempty"`
			CronSpec    string   `json:"cron_spec,omitempty"`
			Prompts     []string `json:"prompts"`
			Timeout     string   `json:"timeout"`
			Active     bool     `json:"active"`
			CanDelete   bool     `json:"can_delete"`
		}{
			Name:        wakeup.Name(),
			Description: wakeup.Description(),
			CronSpec:    wakeup.CronSpec(),
			Prompts:     prompts,
			Timeout:     wakeup.Task().Timeout.String(),
			Active:     wakeup.Running(),
			CanDelete:   isInternal,
		}

		if fireAt := wakeup.FireAt(); !fireAt.IsZero() {
			result.FireAt = fireAt.Format(time.RFC3339)
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("wakeup_inspect: failed to marshal result: %w", err)
		}

		return string(out), nil
	}
}
