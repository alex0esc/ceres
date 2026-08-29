
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// SubagentTool bundles listing available subagents and submitting tasks
// to them into a single tool, dispatched via the "action" parameter
// ("list" or "call").
type SubagentTool struct {
	timeout time.Duration
}

// NewSubagentTool constructs a SubagentTool, reading all relevant config
// values once up front.
func NewSubagentTool() *SubagentTool {
	timeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "subagent.timeout", "1h"))
	if err != nil {
		log.Fatal("Could not read subagent.timeout from tool config!")
	}

	return &SubagentTool{
		timeout: timeout,
	}
}

func (t *SubagentTool) Name() string {
	return "subagent"
}

func (t *SubagentTool) Description() string {
	return "Lists available subagents or submits tasks to them. " +
		"Use action='list' to see all available subagents together with their descriptions and current status " +
		"(use this first, before calling any subagents). " +
		"Use action='call' with 'tasks' to submit one or more tasks to subagents in parallel and wait until all " +
		"of them have finished. A subagent does not remember what it did before, so the full context must be " +
		"provided in each task. A busy subagent is still callable, but its task may be queued and takes longer." +
		"Its also possible to submit multiple tasks at once to one subagent, all tasks will be queue and executed."
}

func (t *SubagentTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "call"},
				"description": "Which operation to perform: 'list' to list subagents, 'call' to submit tasks to subagents.",
			},
			"tasks": map[string]any{
				"type": "array",
				"description": "Required for action='call': list of tasks to submit, must contain at least 1 entry. " +
					"Ignored for action='list'.",
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
		"required":             []string{"action", "tasks"},
		"additionalProperties": false,
	}
}

type subagentCallTask struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

type subagentToolArgs struct {
	Action string              `json:"action"`
	Tasks  []subagentCallTask `json:"tasks"`
}

// pendingCall pairs a submitted task with the channel that will
// eventually deliver its result.
type pendingCall struct {
	agentName string
	ch        <-chan handles.TaskResult
	err       error // set immediately if the agent name could not be resolved
}

func (t *SubagentTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args subagentToolArgs
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("subagent: invalid arguments: %w", err)
			}
		}

		switch args.Action {
		case "list":
			return subagentList(handle)
		case "call":
			if len(args.Tasks) == 0 {
				return "", fmt.Errorf("subagent: 'tasks' is required and must contain at least 1 entry when action='call'")
			}
			return t.subagentCall(args.Tasks, handle)
		case "":
			return "", fmt.Errorf("subagent: 'action' is required (must be 'list' or 'call')")
		default:
			return "", fmt.Errorf("subagent: unknown action %q (must be 'list' or 'call')", args.Action)
		}
	}
}

// subagentList returns all available subagents (excluding the caller
// itself) together with their description and current status.
func subagentList(handle handles.AgentHandle) (string, error) {
	if len(getSubagents()) == 0 {
		return "No subagents available.", nil
	}
	var sb strings.Builder
	for _, agnt := range getSubagents() {
		if agnt == handle {
			continue
		}
		busy := agnt.State().String()
		fmt.Fprintf(&sb, "- %s: %s (status: %s)\n", agnt.Name(), agnt.Description(), busy)
	}
	return sb.String(), nil
}

// subagentCall submits every task up front so they all run concurrently,
// then waits for all results and formats them into a single string.
func (t *SubagentTool) subagentCall(tasks []subagentCallTask, handle handles.AgentHandle) (string, error) {
	// Step 1: submit every task up front so they all run concurrently.
	pending := make([]pendingCall, 0, len(tasks))
	for _, task := range tasks {
		if task.Agent == handle.Name() {
			pending = append(pending, pendingCall{
				agentName: task.Agent,
				err:       fmt.Errorf("agents calls itself %q", task.Agent),
			})
			continue
		}
		agnt, ok := getSubagents()[task.Agent]
		if !ok {
			pending = append(pending, pendingCall{
				agentName: task.Agent,
				err:       fmt.Errorf("unknown agent %q", task.Agent),
			})
			continue
		}
		sub_task := handles.TaskClearAsk(task.Task, t.timeout)
		ch := agnt.SubmitTask(&sub_task)
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
		filtered := result.Response.Filter(history.EntryTypeAssistent, history.EntryTypeToolCall)
		if len(filtered.Entries) > 0 {
			fmt.Fprintf(&sb, "%s\n\n", filtered.String())
		} else {
			fmt.Fprintf(&sb, "Agent returned an empty result!")
		}
	}
	return sb.String(), nil
}
