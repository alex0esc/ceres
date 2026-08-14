package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// SubagentCall is a tool that lets an agent submit tasks to one or more
// other agents in parallel and wait for all of them to finish.
type SubagentCall struct {}



func (t *SubagentCall) Name() string {
	return "subagent_call"
}

func (t *SubagentCall) Description() string {
	return "Submits one or more tasks to subagents in parallel and waits until all subagents have finished their task." +
		"A subagent does not remember what he did before he gets a task so the full context must be provided." +
		"Busy does not mean a subagent is not callable, however if the agent is busy you task will be queued an may take longer."
}


func (t *SubagentCall) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type":        "array",
				"description": "List of tasks to submit. Must contain at least 1 entry.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent": map[string]any{
							"type":        "string",
							"description": "Name of the subagent that should run this task.",
						},
						"task": map[string]any{
							"type":        "string",
							"description": "The task/instruction to send to the subagent.",
						},
					},
					"required":             []string{"agent", "task"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"tasks"},
		"additionalProperties": false,
	}
}

type subagentCallTask struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

type subagentCallArgs struct {
	Tasks []subagentCallTask `json:"tasks"`
}

// pendingCall pairs a submitted task with the channel that will
// eventually deliver its result.
type pendingCall struct {
	agentName string
	ch        <-chan handles.TaskResult
	err       error // set immediately if the agent name could not be resolved
}

func (t *SubagentCall) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args subagentCallArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if len(args.Tasks) == 0 {
			return "", fmt.Errorf("no tasks provided")
		}

		// Step 1: submit every task up front so they all run concurrently.
		pending := make([]pendingCall, 0, len(args.Tasks))
		for _, task := range args.Tasks {
			agnt, ok := getSubagents()[task.Agent]
			if !ok {
				pending = append(pending, pendingCall{
					agentName: task.Agent,
					err:       fmt.Errorf("unknown agent %q", task.Agent),
				})
				continue
			}

			ch := agnt.SubmitTask(ctx, task.Task, true, 0)

			pending = append(pending, pendingCall{
				agentName: task.Agent,
				ch:        ch,
			})
		}

		// Step 2: wait for all results (channels are buffered, so this
		// simply blocks per-entry until each one is ready).
		var sb strings.Builder
		for _, p := range pending {
			fmt.Fprintf(&sb, "=== %s ===\n", p.agentName)

			if p.err != nil {
				fmt.Fprintf(&sb, "error: %v\n\n", p.err)
				continue
			}

			result := <-p.ch
			if result.Err != nil {
				fmt.Fprintf(&sb, "error: %v\n\n", result.Err)
				continue
			}
			fmt.Fprintf(&sb, "%s\n\n", result.Response)
		}

		return sb.String(), nil
	}
}

func init() {
	tool.Register(&SubagentCall{})
}
