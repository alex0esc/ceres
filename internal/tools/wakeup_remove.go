package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WakeupRemoveTool lets an agent remove its own scheduled wakeups.
// External wakeups (created by the user) cannot be removed.
type WakeupRemoveTool struct{}

// NewWakeupRemoveTool constructs a WakeupRemoveTool.
func NewWakeupRemoveTool() WakeupRemoveTool {
	return WakeupRemoveTool{}
}

func (WakeupRemoveTool) Name() string {
	return "wakeup_remove"
}

func (WakeupRemoveTool) Description() string {
	return "Remove a scheduled wakeup by name. Use wakeup_list first to see which wakeups exist. " +
		"Only wakeups created by this agent (internal wakeups) can be removed. " +
		"External wakeups (created by the user in wakeups.toml) cannot be removed."
}

func (WakeupRemoveTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the wakeup to remove",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func (WakeupRemoveTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		if handle == nil {
			return "", fmt.Errorf("wakeup_remove: agent handle is nil")
		}

		var args struct {
			Name string `json:"name"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("wakeup_remove: invalid arguments: %w", err)
		}

		name := strings.TrimSpace(args.Name)
		if name == "" {
			return "", fmt.Errorf("wakeup_remove: name is required")
		}

		err := handle.WakeupManager().RemovePromptWakeup(name)
		switch {
		case errors.Is(err, wakeup.ErrNotFound):
			return "", fmt.Errorf("wakeup_remove: no wakeup named %q", name)
		case errors.Is(err, wakeup.ErrExternal):
			return "", fmt.Errorf("wakeup_remove: cannot remove external wakeup %q", name)
		case err != nil:
			return "", fmt.Errorf("wakeup_remove: failed to remove wakeup %q: %w", name, err)
		}

		result := struct {
			Status string `json:"status"`
			Name   string `json:"name"`
		}{Status: "removed", Name: name}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("wakeup_remove: failed to marshal result: %w", err)
		}

		return string(out), nil
	}
}
