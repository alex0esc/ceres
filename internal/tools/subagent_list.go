package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// SubagentListTool lets an agent see all available subagents and their status.
type SubagentListTool struct{}

// NewSubagentListTool constructs a SubagentListTool.
func NewSubagentListTool() SubagentListTool {
	return SubagentListTool{}
}

func (SubagentListTool) Name() string {
	return "subagent_list"
}

func (SubagentListTool) Description() string {
	return "List all available subagents with their names, descriptions, and current status. " +
		"Use this before calling subagent_call to see which subagents you can delegate work to."
}

func (SubagentListTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t SubagentListTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		_ = ctx

		if handle == nil {
			return "", fmt.Errorf("subagent_list: agent handle is nil")
		}

		subagents := getSubagents()
		if len(subagents) == 0 {
			return "No subagents available.", nil
		}

		var sb strings.Builder
		for name, agnt := range subagents {
			if agnt == handle {
				continue
			}
			status := agnt.State().String()
			fmt.Fprintf(&sb, "- %s: %s (status: %s)\n", name, agnt.Description(), status)
		}

		return sb.String(), nil
	}
}
