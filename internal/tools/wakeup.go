package tools


import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WakeupTool lets an agent list, create, and remove its own scheduled wakeups.
// Whether it may create recurring (cron-based) wakeups, as opposed to only one-shot ones, is controlled via config.
// The timeout for wakeups is controlled by the user via configuration and cannot be chosen by the agent.
type WakeupTool struct {
	canCreateRepeatable bool
	defaultTzName       string
	timeout             time.Duration
}

// NewWakeupTool constructs a WakeupTool, reading all relevant config values once up front.
// The wakeup timeout is configured via "wakeup.timeout" and defaults to one hour if no valid value is configured.
func NewWakeupTool() WakeupTool {
	cfg := tool.GetToolConfig()

	tzName := config.ReadEntry(cfg, "timezone", "Local")
	canCreateRepeatable := config.ReadEntry(cfg, "wakeup.can_create_repeatable", false)
	timeoutRaw := config.ReadEntry(cfg, "wakeup.timeout", "3h")

	timeout, err := time.ParseDuration(timeoutRaw)
	if err != nil || timeout <= 0 {
		timeout = time.Hour
	}

	return WakeupTool{
		canCreateRepeatable: canCreateRepeatable,
		defaultTzName:       tzName,
		timeout:             timeout,
	}
}

func (WakeupTool) Name() string {
	return "wakeup"
}

func (t WakeupTool) Description() string {
	repeatNote := "You are not permitted to create recurring wakeups; cron_spec must be left empty and only fire_at may be used."
	if t.canCreateRepeatable {
		repeatNote = "You are permitted to create both one-shot (fire_at) and recurring (cron_spec) wakeups. One of the two fields must be empty."
	}

	return fmt.Sprintf("Tool to manage scheduled wakeups. "+
			"Use action='list' to see all currently scheduled wakeups, including protected user-created wakeups which cannot be removed or edited. List all wakeups before trying to add or remove any wakeups!"+
			"Use action='add' to create a new one-shot or recurring wakeup. Name, description, and at least one prompt are required, plus exactly one of fire_at or cron_spec. "+
			"When the wakeup fires, the prompts are sent to the agent one after another in order, in the same fresh context, so the agent sees and remembers what it did and produced in the earlier prompts of this same wakeup — later prompts can build on that. "+
			"The wakeup starts in a completely fresh context with no memory of the current conversation or any other wakeup, so the first prompt must be fully self-contained and include the complete context, all relevant information, the concrete goal, constraints, and expected result needed to perform it correctly. "+
			"The user controls the timeout and the agent cannot choose or override it. Every wakeup created by this tool uses a timeout of %s. %s "+
			"Use action='remove' to cancel an existing wakeup by name; protected wakeups cannot be removed this way. "+
			"Always use %s as timezone for fire_at timestamps; cron_spec also automaticaly uses %s as timezone.",
		t.timeout.String(),
		repeatNote,
		t.defaultTzName,
		t.defaultTzName,
	)
}

func (WakeupTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "add", "remove"},
				"description": "Which operation to perform. Use list before add or remove!",
			},
			"name": map[string]any{
				"type":        []string{"string", "null"},
				"description": "Unique name of the wakeup. Required for \"add\" and \"remove\"; ignored for \"list\".",
			},
			"description": map[string]any{
				"type":        []string{"string", "null"},
				"description": "Short description of what this wakeup is for and what it should do when it fires. Required for \"add\"; ignored otherwise.",
			},
			"prompts": map[string]any{
				"type":        []string{"array", "null"},
				"items":       map[string]any{"type": "string"},
				"description": "One or more prompts sent to the agent in order, one after another, within the same fresh context when the wakeup fires. The agent sees and remembers what it did and produced in the earlier prompts of this list, so later prompts can build on that. The first prompt must be fully self-contained: the agent has no memory of the current conversation or any other wakeup, so it must include all context, relevant details, the concrete goal, constraints, and expected result needed to get started. Required for \"add\" and must contain at least one entry.",
			},
			"fire_at": map[string]any{
				"type":        []string{"string", "null"},
				"description": "RFC3339 timestamp (e.g. \"2026-10-24T22:05:00+02:00\") for a one-shot wakeup. For \"add\", exactly one of fire_at or cron_spec must be set. Use the configured timezone.",
			},
			"cron_spec": map[string]any{
				"type":        []string{"string", "null"},
				"description": "Cron schedule (e.g. \"0 9 * * 1-5\") for a recurring wakeup. For \"add\", exactly one of fire_at or cron_spec must be set. The schedule uses the configured timezone.",
			},
		},
		"required":             []string{"action", "name", "description", "prompts", "fire_at", "cron_spec"},
		"additionalProperties": false,
	}
}

func (t WakeupTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		var args struct {
			Action      string   `json:"action"`
			Name        *string  `json:"name"`
			Description *string  `json:"description"`
			Prompts     []string `json:"prompts"`
			FireAt      *string  `json:"fire_at"`
			CronSpec    *string  `json:"cron_spec"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("wakeup: invalid arguments: %w", err)
		}

		if args.FireAt != nil && *args.FireAt == "null" {
			*args.FireAt = ""
		}
		if args.CronSpec != nil && *args.CronSpec == "null" {
			*args.CronSpec = ""
		}

		switch args.Action {
		case "list":
			return t.handleList(handle)
		case "add":
			return t.handleAdd(handle, args.Name, args.Description, args.Prompts, args.FireAt, args.CronSpec)
		case "remove":
			return t.handleRemove(handle, args.Name)
		default:
			return "", fmt.Errorf("wakeup: unknown action %q, must be one of: list, add, remove", args.Action)
		}
	}
}

func (t WakeupTool) handleList(handle handles.AgentHandle) (string, error) {
	if handle == nil {
		return "", fmt.Errorf("wakeup: agent handle is nil")
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

		e := entry{Name: w.Name(), Description: w.Description(), CronSpec: w.CroneSpec(), Protected: w.Protected()}
		if fireAt := w.FireAt(); fireAt != nil {
			e.FireAt = fireAt.Format(time.RFC3339)
		}

		out = append(out, e)
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("wakeup: failed to marshal result: %w", err)
	}

	return string(result), nil
}

func (t WakeupTool) handleAdd(handle handles.AgentHandle, name, description *string, prompts []string, fireAtRaw, cronSpecRaw *string) (string, error) {
	if handle == nil {
		return "", fmt.Errorf("wakeup: agent handle is nil")
	}

	if name == nil || strings.TrimSpace(*name) == "" {
		return "", fmt.Errorf("wakeup: name is required for action \"add\"")
	}
	if description == nil || strings.TrimSpace(*description) == "" {
		return "", fmt.Errorf("wakeup: description is required for action \"add\"")
	}
	if len(prompts) == 0 {
		return "", fmt.Errorf("wakeup: prompts must contain at least one entry for action \"add\"")
	}

	hasFireAt := fireAtRaw != nil && strings.TrimSpace(*fireAtRaw) != ""
	hasCron := cronSpecRaw != nil && strings.TrimSpace(*cronSpecRaw) != ""

	if hasFireAt == hasCron {
		return "", fmt.Errorf("wakeup: exactly one of fire_at or cron_spec must be set for action \"add\"")
	}
	if hasCron && !t.canCreateRepeatable {
		return "", fmt.Errorf("wakeup: this agent is not allowed to create recurring wakeups")
	}

	var fireAt *time.Time
	cronSpec := ""

	if hasFireAt {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*fireAtRaw))
		if err != nil {
			return "", fmt.Errorf("wakeup: invalid fire_at %q, expected RFC3339 (e.g. \"2026-10-24T22:05:00+02:00\"): %w", *fireAtRaw, err)
		}
		fireAt = &parsed
	} else {
		cronSpec = strings.TrimSpace(*cronSpecRaw)
	}

	// Protected is always false here - only the system creates protected wakeups, never the agent itself via this tool.
	w := wakeup.NewWakeUp(*name, *description, handle, fireAt, cronSpec, prompts, t.timeout, false)

	if err := handle.AddWakeup(w); err != nil {
		return "", fmt.Errorf("wakeup: failed to set wakeup: %w", err)
	}

	result := struct {
		Status string `json:"status"`
		Name   string `json:"name"`
	}{Status: "created", Name: *name}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("wakeup: failed to marshal result: %w", err)
	}

	return string(out), nil
}

func (t WakeupTool) handleRemove(handle handles.AgentHandle, name *string) (string, error) {
	if handle == nil {
		return "", fmt.Errorf("wakeup: agent handle is nil")
	}

	if name == nil || strings.TrimSpace(*name) == "" {
		return "", fmt.Errorf("wakeup: name is required for action \"remove\"")
	}

	wakeupName := strings.TrimSpace(*name)

	wu := handle.GetWakeup(wakeupName)
	if wu == nil {
		return "", fmt.Errorf("wakeup: no wakeup named %q", wakeupName)
	}

	if wu.Protected() || !handle.RemoveWakeup(wakeupName) {
		return "", fmt.Errorf("wakeup: could not remove wakeup %q (it is probably protected)", wakeupName)
	}

	result := struct {
		Status string `json:"status"`
		Name   string `json:"name"`
	}{Status: "removed", Name: wakeupName}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("wakeup: failed to marshal result: %w", err)
	}

	return string(out), nil
}
