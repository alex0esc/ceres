package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// GetTimeTool returns the current time, optionally for a given IANA
// timezone (e.g. "Europe/Berlin", "UTC"). Defaults to UTC if no
// timezone is given or if the given timezone is invalid.
type GetTimeTool struct{}

// NewGetTimeTool constructs a GetTimeTool.
func NewGetTimeTool() GetTimeTool {
	return GetTimeTool{}
}

func (GetTimeTool) Name() string {
	return "get_time"
}

func (GetTimeTool) Description() string {
	return "Returns the current date and time. Optionally accepts an IANA timezone name (e.g. 'Europe/Berlin', 'America/New_York', 'UTC'); defaults to UTC if omitted." +
		   "ALWAYS use this tool to verify what is meant by today (e.g if the user sais 'what ... today?')!"
}

func (GetTimeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{
				"type":        []string{"string", "null"},
				"description": "IANA timezone name, e.g. 'Europe/Berlin'. Defaults to UTC if omitted.",
			},
		},
		"required":             []string{"timezone"},
		"additionalProperties": false,
	}
}

func (GetTimeTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Timezone string `json:"timezone"`
		}
		// argumentsJSON may legitimately be empty ("" or "{}") since
		// "timezone" is optional.
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("get_time: invalid arguments: %w", err)
			}
		}
		loc := time.UTC
		tzName := "UTC"
		if args.Timezone != "" {
			l, err := time.LoadLocation(args.Timezone)
			if err != nil {
				return "", fmt.Errorf("get_time: unknown timezone %q: %w", args.Timezone, err)
			}
			loc = l
			tzName = args.Timezone
		}
		now := time.Now().In(loc)
		out := struct {
			Timezone  string `json:"timezone"`
			RFC3339   string `json:"rfc3339"`
			Unix      int64  `json:"unix"`
			Formatted string `json:"formatted"`
		}{
			Timezone:  tzName,
			RFC3339:   now.Format(time.RFC3339),
			Unix:      now.Unix(),
			Formatted: now.Format("Monday, 02 January 2006 15:04:05 MST"),
		}
		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("get_time: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}
