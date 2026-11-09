package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/appconfig"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WakeupAddTool lets an agent create new scheduled wakeups. Whether it may
// create recurring (cron-based) wakeups, the wakeup timeout, and whether the
// wakeup keeps the conversation history are configured per agent.
type WakeupAddTool struct {
	defaultTzName string
}

// NewWakeupAddTool constructs a WakeupAddTool. The timezone is global
// (app config); the per-agent values are read in the handler.
func NewWakeupAddTool() WakeupAddTool {
	tzName := config.ReadEntry(appconfig.Get(), "timezone", "Local")

	return WakeupAddTool{
		defaultTzName: tzName,
	}
}

func (WakeupAddTool) Name() string {
	return "wakeup_add"
}

func (t WakeupAddTool) Description() string {
	return fmt.Sprintf("Create a new scheduled wakeup for this agent. Use wakeup_list first to see existing wakeups and avoid name conflicts. "+
		"Required: name (unique), description, a prompt, and exactly one of fire_at or cron_spec. "+
		"Recurring wakeups can be disabled per agent (tool_settings.wakeup.can_create_repeatable); when disabled, only fire_at may be used and cron_spec must be empty. "+
		"Whether the wakeup starts with a fresh context or keeps the conversation history is configured per agent (tool_settings.wakeup.clear_history); when the history is cleared, the prompt must be fully self-contained with all context, goal, constraints, and expected result. "+
		"The wakeup timeout is configured per agent (tool_settings.wakeup.timeout) and cannot be changed via arguments. "+
		"Always use %s timezone for fire_at.",
		t.defaultTzName,
	)
}

func (WakeupAddTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Unique name for this wakeup",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Short description of what this wakeup does",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The prompt message sent when the wakeup fires.",
			},
			"fire_at": map[string]any{
				"type":        "string",
				"description": "RFC3339 timestamp for one-shot wakeup (e.g. \"2026-10-24T22:05:00+02:00\"). Mutually exclusive with cron_spec.",
			},
			"cron_spec": map[string]any{
				"type":        "string",
				"description": "Cron schedule for recurring wakeup (e.g. \"0 9 * * 1-5\"). Mutually exclusive with fire_at.",
			},
		},
		"required":             []string{"name", "description", "prompt"},
		"additionalProperties": false,
	}
}

func (t WakeupAddTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		if handle == nil {
			return "", fmt.Errorf("wakeup_add: agent handle is nil")
		}

		cfg := handle.Config()
		canCreateRepeatable := config.ReadEntry(cfg, "tool_settings.wakeup.can_create_repeatable", false)
		wakeupTimeout := config.ReadEntry(cfg, "tool_settings.wakeup.timeout", time.Hour)
		clearHistory := config.ReadEntry(cfg, "tool_settings.wakeup.clear_history", true)

		var args struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Prompt      string `json:"prompt"`
			FireAt      string `json:"fire_at"`
			CronSpec    string `json:"cron_spec"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("wakeup_add: invalid arguments: %w", err)
		}

		name := strings.TrimSpace(args.Name)
		if name == "" {
			return "", fmt.Errorf("wakeup_add: name is required")
		}

		description := strings.TrimSpace(args.Description)
		if description == "" {
			return "", fmt.Errorf("wakeup_add: description is required")
		}

		prompt := strings.TrimSpace(args.Prompt)
		if prompt == "" {
			return "", fmt.Errorf("wakeup_add: prompt is required")
		}

		hasFireAt := strings.TrimSpace(args.FireAt) != ""
		hasCron := strings.TrimSpace(args.CronSpec) != ""

		if hasFireAt == hasCron {
			return "", fmt.Errorf("wakeup_add: exactly one of fire_at or cron_spec must be set")
		}
		if hasCron && !canCreateRepeatable {
			return "", fmt.Errorf("wakeup_add: creating repeatable wakeups is disabled for this agent (tool_settings.wakeup.can_create_repeatable)")
		}

		var fireAt *time.Time
		cronSpec := ""

		if hasFireAt {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(args.FireAt))
			if err != nil {
				return "", fmt.Errorf("wakeup_add: invalid fire_at %q, expected RFC3339: %w", args.FireAt, err)
			}
			fireAt = &parsed
		} else {
			cronSpec = strings.TrimSpace(args.CronSpec)
		}

		_, err := handle.WakeupManager().AddPromptWakeup(
			name,
			description,
			fireAt,
			cronSpec,
			prompt,
			wakeupTimeout,
			clearHistory,
		)
		switch {
		case errors.Is(err, wakeup.ErrExists):
			return "", fmt.Errorf("wakeup_add: a wakeup named %q already exists", name)
		case err != nil:
			return "", fmt.Errorf("wakeup_add: failed to create wakeup: %w", err)
		}

		result := struct {
			Status string `json:"status"`
			Name   string `json:"name"`
		}{Status: "created", Name: name}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("wakeup_add: failed to marshal result: %w", err)
		}

		return string(out), nil
	}
}
