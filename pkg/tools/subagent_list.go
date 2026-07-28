package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/handles"
)

// SubagentList is a tool that lists all available subagents
// together with their description.
type SubagentList struct {
	agents []handles.AgentHandle
}

func (t *SubagentList) SetSubagentList(agents []handles.AgentHandle) {
	t.agents = agents
}

func (t *SubagentList) Name() string {
	return "subagent_list"
}

func (t *SubagentList) Description() string {
	return "Lists all available subagents together with their descriptions and current status." +
		"Only use this tool before calling any subagents."
}

func (t *SubagentList) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func (t *SubagentList) Handler() ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		if len(t.agents) == 0 {
			return "No subagents available.", nil
		}

		var sb strings.Builder
		for _, agnt := range t.agents {
			busy := "waiting"
			if agnt.Busy() {
				busy = "busy"
			}
			fmt.Fprintf(&sb, "- %s: %s (status: %s)\n", agnt.Name(), agnt.Description(), busy)
		}
		return sb.String(), nil
	}
}



func init() {
	Register(&SubagentList{})
}
