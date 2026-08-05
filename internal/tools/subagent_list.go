package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/tool"
)

// SubagentList is a tool that lists all available subagents
// together with their description.
type SubagentList struct {}


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

func (t *SubagentList) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		if len(getSubagents()) == 0 {
			return "No subagents available.", nil
		}

		var sb strings.Builder
		for _, agnt := range getSubagents() {
			busy := agnt.State().String()
			fmt.Fprintf(&sb, "- %s: %s (status: %s)\n", agnt.Name(), agnt.Description(), busy)
		}
		return sb.String(), nil
	}
}



func init() {
	tool.Register(&SubagentList{})
}
