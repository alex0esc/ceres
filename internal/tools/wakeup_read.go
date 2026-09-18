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

// WakeupReadTool lets an agent list its own scheduled wakeups (including
// protected, user-created ones which it cannot edit or remove).
type WakeupReadTool struct{}

// NewWakeupReadTool constructs a WakeupReadTool.
func NewWakeupReadTool() WakeupReadTool {
	return WakeupReadTool{}
}

func (WakeupReadTool) Name() string {
	return "wakeup_read"
}

func (WakeupReadTool) Description() string {
	return "Tool to inspect scheduled wakeups. " +
		"Use action='list' to see all currently scheduled wakeups, including protected user-created wakeups which cannot be removed or edited. " +
		"Use action='get_prompts' to read the full prompt chain of one specific wakeup by name, e.g. to review, reuse, or double-check what it will do when it fires. " +
		"Always list wakeups with this tool before trying to add or remove any wakeup via wakeup_edit!"
}

func (WakeupReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "get_prompts"},
				"description": "Which operation to perform. 'list' lists all wakeups; 'get_prompts' returns the prompts of a single wakeup given its name.",
			},
			"name": map[string]any{
				"type":        []string{"string", "null"},
				"description": "Name of the wakeup to read prompts from. Required for \"get_prompts\"; ignored for \"list\".",
			},
		},
		"required":             []string{"action", "name"},
		"additionalProperties": false,
	}
}

func (t WakeupReadTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		var args struct {
			Action string  `json:"action"`
			Name   *string `json:"name"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("wakeup_read: invalid arguments: %w", err)
		}

		switch args.Action {
		case "list":
			return t.handleList(handle)
		case "get_prompts":
			return t.handleGetPrompts(handle, args.Name)
		default:
			return "", fmt.Errorf("wakeup_read: unknown action %q, must be one of: list, get_prompts", args.Action)
		}
	}
}

func (t WakeupReadTool) handleList(handle handles.AgentHandle) (string, error) {
	if handle == nil {
		return "", fmt.Errorf("wakeup_read: agent handle is nil")
	}

	wakeups := handle.ListWakeups()

	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		FireAt      string `json:"fire_at,omitempty"`
		CronSpec    string `json:"cron_spec,omitempty"`
		Protected   bool   `json:"protected"`
	}

	out := make([]entry, 0, len(wakeups))

	for _, w := range wakeups {
		if w == nil {
			continue
		}

		e := entry{Name: w.Name(), Description: w.Description(), CronSpec: w.CronSpec(), Protected: w.Protected()}
		if fireAt := w.FireAt(); fireAt != nil {
			e.FireAt = fireAt.Format(time.RFC3339)
		}

		out = append(out, e)
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("wakeup_read: failed to marshal result: %w", err)
	}

	return string(result), nil
}

func (t WakeupReadTool) handleGetPrompts(handle handles.AgentHandle, name *string) (string, error) {
	if handle == nil {
		return "", fmt.Errorf("wakeup_read: agent handle is nil")
	}

	if name == nil || strings.TrimSpace(*name) == "" {
		return "", fmt.Errorf("wakeup_read: name is required for action \"get_prompts\"")
	}

	wakeupName := strings.TrimSpace(*name)

	w := handle.GetWakeup(wakeupName)
	if w == nil {
		return "", fmt.Errorf("wakeup_read: no wakeup named %q", wakeupName)
	}

	result := struct {
		Prompts []string `json:"prompts"`
	}{Prompts: w.Prompts()}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("wakeup_read: failed to marshal result: %w", err)
	}

	return string(out), nil
}
