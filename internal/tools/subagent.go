
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// SubagentTool bundles listing available subagents and submitting tasks to them into a single tool, dispatched via the "action" parameter ("list" or "call").
// The timeout for subagent calls is controlled by configuration and cannot be chosen by the agent.
type SubagentTool struct {
	summarizePrompt handles.Prompt
	timeout         time.Duration
}

// NewSubagentTool constructs a SubagentTool, reading all relevant config values once up front.
func NewSubagentTool() *SubagentTool {
	cfg := tool.GetToolConfig()

	timeout := config.ReadEntry(cfg, "subagent.timeout", time.Hour*1)
	summarizePrompt := config.ReadEntry(cfg, "subagent.summarize_prompt",
		"Summarize all actions and results that have been achieved in the current chat session for your orchestrator agent. "+
			"IMPORTANT: The summary should contain everything that is needed by the orchestrator agent to evaluate your work. The orchestrator agent should receive a clean, complete answer that it can directly work with.",
	)

	return &SubagentTool{
		timeout:         timeout,
		summarizePrompt: handles.Prompt{Text: summarizePrompt},
	}
}

func (t *SubagentTool) Name() string {
	return "subagent"
}

func (t *SubagentTool) Description() string {
	return fmt.Sprintf(
		"Use action='list' to see all available subagents together with their descriptions and current status; use this first before calling a subagent. "+
			"Use action='call' with 'tasks' to submit one or more tasks to subagents in parallel. This blocks until all submitted tasks have finished. "+
			"Each entry in tasks contains an agent and one or more task prompts that are sent to that subagent in order. "+
			"Every individual task prompt must be fully self-contained and include the complete context, all relevant information, the concrete objective, constraints, assumptions, and expected result needed by the subagent to perform it correctly. "+
			"Never assume that a subagent remembers context from another task, a previous task, another call, or an earlier conversation unless that context is explicitly included again in the current task prompt. "+
			"Tasks are useful for splitting a complex piece of work into smaller independent steps, but every step must still contain all context required for that step. "+
			"A busy subagent is still callable, but its task will be queued and the response may take longer. "+
			"The timeout for every subagent call is configured by the user and cannot be chosen or overridden by the agent; the configured timeout is %s. "+
			"The returned message from a subagent is a summary of its actions and results, not necessarily its exact generated response.",
		t.timeout.String(),
	)
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
				"description": "Required for action='call'. List of one or more tasks to submit to subagents. Every task must identify exactly one agent and contain one or more fully self-contained task prompts. Each prompt must repeat all context and requirements needed for that specific step; do not rely on context from another task, previous task, previous call, or earlier conversation. Ignored for action='list'.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent": map[string]any{
							"type":        "string",
							"description": "Name of the subagent that should run this task.",
						},
						"tasks": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
							"description": "One or more prompts to send to the selected subagent in order. Every prompt must be fully self-contained and include the complete context, relevant details, concrete objective, constraints, assumptions, and expected result. The subagent must not need information from previous prompts or previous calls to understand the current task.",
						},
					},
					"required":             []string{"agent", "tasks"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"action", "tasks"},
		"additionalProperties": false,
	}
}

type subagentCallTask struct {
	Agent string   `json:"agent"`
	Tasks []string `json:"tasks"`
}

type subagentToolArgs struct {
	Action string              `json:"action"`
	Tasks  []subagentCallTask `json:"tasks"`
}

// pendingCall pairs a submitted task with the channel that will eventually deliver its result.
type pendingCall struct {
	agentName string
	ch        <-chan handles.TaskResult
	err       error
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
			return t.subagentCall(ctx, args.Tasks, handle)
		case "":
			return "", fmt.Errorf("subagent: 'action' is required (must be 'list' or 'call')")
		default:
			return "", fmt.Errorf("subagent: unknown action %q (must be 'list' or 'call')", args.Action)
		}
	}
}

// subagentList returns all available subagents (excluding the caller itself) together with their description and current status.
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

// subagentCall submits every task up front so they all run concurrently, then waits for all results and formats them into a single string.
func (t *SubagentTool) subagentCall(ctx context.Context, tasks []subagentCallTask, handle handles.AgentHandle) (string, error) {
	// Step 1: submit every task up front so they all run concurrently.
	pending := make([]pendingCall, 0, len(tasks))

	for _, task := range tasks {
		if task.Agent == handle.Name() {
			pending = append(pending, pendingCall{
				agentName: task.Agent,
				err:       fmt.Errorf("agent calls itself %q", task.Agent),
			})
			continue
		}

		if len(task.Tasks) == 0 {
			pending = append(pending, pendingCall{
				agentName: task.Agent,
				err:       fmt.Errorf("task for agent %q must contain at least 1 task prompt", task.Agent),
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

		prompts := make([]handles.Prompt, 0, len(task.Tasks)+1)

		for _, prompt := range task.Tasks {
			if strings.TrimSpace(prompt) == "" {
				pending = append(pending, pendingCall{
					agentName: task.Agent,
					err:       fmt.Errorf("task for agent %q contains an empty task prompt", task.Agent),
				})
				continue
			}

			prompts = append(prompts, handles.Prompt{Text: prompt})
		}

		if len(prompts) == 0 {
			continue
		}

		prompts = append(prompts, t.summarizePrompt)

		subTask := handles.TaskClearAskMultiple(prompts, t.timeout)
		subTask.ParentCtx = ctx

		ch := agnt.SubmitTask(subTask)
		pending = append(pending, pendingCall{
			agentName: task.Agent,
			ch:        ch,
		})
	}

	// Step 2: wait for all results. Channels are buffered, so this simply blocks per-entry until each one is ready.
	var sb strings.Builder

	for _, p := range pending {
		fmt.Fprintf(&sb, "=== %s ===\n", p.agentName)

		if p.err != nil {
			fmt.Fprintf(&sb, "error: %v\n\n", p.err)
			continue
		}

		result := <-p.ch

		if result.Err != nil {
			fmt.Fprintf(&sb, "Agent returned an error: %v\n\n", result.Err)
			continue
		}

		if result.Interrupted {
			fmt.Fprintf(&sb, "Agent was interrupted!\n\n")
			continue
		}

		filtered := result.Response.Filter(history.EntryTypeAssistent)

		if len(filtered.Entries) > 0 {
			fmt.Fprintf(&sb, "%s\n\n", filtered.LastEntry().String())
		} else {
			fmt.Fprintf(&sb, "Agent returned an empty result!\n\n")
		}
	}

	return sb.String(), nil
}
