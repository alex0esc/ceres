package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// SubagentCallTool lets an agent delegate tasks to subagents and wait for their responses.
// Multiple tasks can be submitted and run in parallel.
type SubagentCallTool struct {
	timeout time.Duration
}

// NewSubagentCallTool constructs a SubagentCallTool, reading the timeout config up front.
func NewSubagentCallTool() *SubagentCallTool {
	cfg := tool.GetToolConfig()
	timeout := config.ReadEntry(cfg, "subagent.timeout", time.Hour*1)

	return &SubagentCallTool{
		timeout: timeout,
	}
}

func (t *SubagentCallTool) Name() string {
	return "subagent_call"
}

func (t *SubagentCallTool) Description() string {
	return fmt.Sprintf("Delegate tasks to subagents and wait for their responses. "+
		"Submit one or more tasks to run in parallel. Each task specifies which subagent should execute it and the prompt to send. "+
		"Tasks are fully independent - include all necessary context in each prompt, as subagents have no memory of other tasks or conversations. "+
		"A busy subagent queues the task. Timeout per task is %s. "+
		"You receive the subagent's final response message - not its intermediate steps or tool calls. "+
		"Use subagent_list first to see available subagents.",
		t.timeout.String(),
	)
}

func (t *SubagentCallTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type":        "array",
				"minItems":    1,
				"description": "List of tasks to delegate. Each task has an 'agent' (subagent name) and a 'prompt' (the task to execute). Tasks run in parallel.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent": map[string]any{
							"type":        "string",
							"description": "Name of the subagent to execute this task. Use subagent_list to see available names.",
						},
						"prompt": map[string]any{
							"type":        "string",
							"description": "The complete task description with all necessary context. Must be self-contained.",
						},
					},
					"required":             []string{"agent", "prompt"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"tasks"},
		"additionalProperties": false,
	}
}

type subagentTask struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
}

type subagentCallArgs struct {
	Tasks []subagentTask `json:"tasks"`
}

type callResult struct {
	agent  string
	output string
	err    error
}

func (t *SubagentCallTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		if handle == nil {
			return "", fmt.Errorf("subagent_call: agent handle is nil")
		}

		var args subagentCallArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("subagent_call: invalid arguments: %w", err)
		}

		if len(args.Tasks) == 0 {
			return "", fmt.Errorf("subagent_call: 'tasks' must contain at least 1 entry")
		}

		return t.callSubagents(ctx, args.Tasks, handle)
	}
}

// callSubagents submits all tasks in parallel using goroutines, collects results,
// and returns them in the same order as submitted.
func (t *SubagentCallTool) callSubagents(ctx context.Context, tasks []subagentTask, handle handles.AgentHandle) (string, error) {
	results := make([]callResult, len(tasks))
	var wg sync.WaitGroup

	subagents := getSubagents()

	for i, tsk := range tasks {
		wg.Add(1)
		go func(idx int, item subagentTask) {  // <-- Umbenannt zu "item"
			defer wg.Done()

			// Validate: agent must not call itself
			if item.Agent == handle.Name() {
				results[idx] = callResult{agent: item.Agent, err: fmt.Errorf("agent cannot call itself")}
				return
			}

			// Validate: prompt must not be empty
			prompt := strings.TrimSpace(item.Prompt)
			if prompt == "" {
				results[idx] = callResult{agent: item.Agent, err: fmt.Errorf("prompt must not be empty")}
				return
			}

			// Find the subagent
			agnt, ok := subagents[item.Agent]
			if !ok {
				results[idx] = callResult{agent: item.Agent, err: fmt.Errorf("unknown subagent %q", item.Agent)}
				return
			}

			// Build the task - ClearAsk so it starts fresh and only returns the final response
			subTask := task.TaskClearAskMultiple(  // <-- "task" ist jetzt wieder das Package
				[]task.Prompt{{Text: prompt}},
				t.timeout,
			)
			subTask.ParentCtx = ctx

			// Submit and wait
			ch := agnt.SubmitTask(subTask)
			res := <-ch

			if res.Err != nil {
				results[idx] = callResult{agent: item.Agent, err: res.Err}
				return
			}

			if res.Interrupted {
				results[idx] = callResult{agent: item.Agent, err: fmt.Errorf("subagent was interrupted")}
				return
			}

			// Extract only the final assistant message
			filtered := res.Response.Filter(history.EntryTypeAssistant)
			if len(filtered.Entries) == 0 {
				results[idx] = callResult{agent: item.Agent, output: "(empty response)"}
				return
			}

			_, last := filtered.LastEntry()
			results[idx] = callResult{agent: item.Agent, output: last.String()}
		}(i, tsk)
	}

	wg.Wait()

	// Format all results
	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "=== %s ===\n", r.agent)
		if r.err != nil {
			fmt.Fprintf(&sb, "error: %v\n\n", r.err)
		} else {
			fmt.Fprintf(&sb, "%s\n\n", r.output)
		}
	}

	return sb.String(), nil
}
