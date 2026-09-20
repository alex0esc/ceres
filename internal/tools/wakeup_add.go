package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WakeupAddTool lets an agent create new scheduled wakeups.
// Whether it may create recurring (cron-based) wakeups is controlled via config.
// The timeout for wakeups is controlled by the user via configuration and cannot be chosen by the agent.
type WakeupAddTool struct {
	canCreateRepeatable bool
	defaultTzName       string
	timeout             time.Duration
	clearHistory        bool
}

// NewWakeupAddTool constructs a WakeupAddTool, reading all relevant config values once up front.
func NewWakeupAddTool() WakeupAddTool {
	cfg := tool.GetToolConfig()

	tzName := config.ReadEntry(cfg, "timezone", "Local")
	canCreateRepeatable := config.ReadEntry(cfg, "wakeup.can_create_repeatable", false)
	timeout := config.ReadEntry(cfg, "wakeup.timeout", time.Hour)
	clearHistory := config.ReadEntry(cfg, "wakeup.clear_history", true)


	return WakeupAddTool{
		canCreateRepeatable: canCreateRepeatable,
		defaultTzName:       tzName,
		timeout:             timeout,
		clearHistory:        clearHistory,
	}
}

func (WakeupAddTool) Name() string {
	return "wakeup_add"
}

func (t WakeupAddTool) Description() string {
	repeatNote := "You are not permitted to create recurring wakeups; only fire_at may be used, cron_spec must be empty."
	if t.canCreateRepeatable {
		repeatNote = "You may create both one-shot (fire_at) and recurring (cron_spec) wakeups. Exactly one must be set."
	}

	contextNote := "The wakeup starts with a completely fresh context - no memory of current conversation or other wakeups. Prompts must be fully self-contained with all context, goal, constraints, and expected result."
	if !t.clearHistory {
		contextNote = "The wakeup keeps the agent's existing conversation history, allowing it to build on previous context. Prompts can reference earlier conversations and decisions."
	}

	return fmt.Sprintf("Create a new scheduled wakeup for this agent. Use wakeup_list first to see existing wakeups and avoid name conflicts. "+
		"Required: name (unique), description, at least one prompt, and exactly one of fire_at or cron_spec. "+
		"%s "+
		"Timeout is %s (controlled by user, cannot be changed). Always use %s timezone for fire_at. %s",
		contextNote,
		t.timeout.String(),
		t.defaultTzName,
		repeatNote,
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
			"prompts": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"description": "One or more prompts combined into a single message when the wakeup fires.",
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
		"required":             []string{"name", "description", "prompts"},
		"additionalProperties": false,
	}
}

func (t WakeupAddTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		if handle == nil {
			return "", fmt.Errorf("wakeup_add: agent handle is nil")
		}

		var args struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Prompts     []string `json:"prompts"`
			FireAt      string   `json:"fire_at"`
			CronSpec    string   `json:"cron_spec"`
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

		prompt := joinPrompts(args.Prompts)
		if prompt == "" {
			return "", fmt.Errorf("wakeup_add: prompts must contain at least one non-empty entry")
		}

		hasFireAt := strings.TrimSpace(args.FireAt) != ""
		hasCron := strings.TrimSpace(args.CronSpec) != ""

		if hasFireAt == hasCron {
			return "", fmt.Errorf("wakeup_add: exactly one of fire_at or cron_spec must be set")
		}
		if hasCron && !t.canCreateRepeatable {
			return "", fmt.Errorf("wakeup_add: this agent is not allowed to create recurring wakeups")
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
			t.timeout,
			t.clearHistory,
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

// joinPrompts combines the prompts into the single prompt a wakeup stores: empty
// entries are dropped, the rest is joined in order, separated by a blank line.
func joinPrompts(prompts []string) string {
	parts := make([]string, 0, len(prompts))
	for _, p := range prompts {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}
