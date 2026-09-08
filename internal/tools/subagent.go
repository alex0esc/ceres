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
// Every task the calling agent submits must include its own summary_instructions, since the calling
// agent only ever sees the subagent's final summary, never its raw actions or intermediate steps.
type SubagentTool struct {
	timeout time.Duration
}

// NewSubagentTool constructs a SubagentTool, reading all relevant config values once up front.
func NewSubagentTool() *SubagentTool {
	cfg := tool.GetToolConfig()

	timeout := config.ReadEntry(cfg, "subagent.timeout", time.Hour*1)

	return &SubagentTool{
		timeout: timeout,
	}
}

func (t *SubagentTool) Name() string {
	return "subagent"
}

func (t *SubagentTool) Description() string {
	return fmt.Sprintf(
		"Use action='list' to see all available subagents together with their descriptions and current status; use this first before calling a subagent. "+
			"Use action='call' with 'tasks' to submit one or more tasks to subagents in parallel. This blocks until all submitted tasks have finished. "+
			"Each entry in tasks contains an agent, one or more task prompts, and a summary_instructions string. "+
			"Every task must be fully self-contained and include the complete context, all relevant information, the concrete objective, constraints, assumptions, and expected result needed by the subagent to perform it correctly. "+
			"Never assume that a subagent remembers context from another task, or an earlier conversation unless that context is explicitly included again in the task. " +
			"One task can contain multiple prompts which are NOT independent of each other. This makes it possible to seperate one complex task into multiple smaller steps the subagent can work thorugh. " +
			"For parallel execution you have to use multiple subagents in one call, because the subagent call blocks until all agents are finished. For optimal parallelism split the work into tasks of equal size." +
			"A busy subagent is still callable, but its task will be queued and the response may take longer. The timeout for every subagent call is %s. "+
			"IMPORTANT: You will only ever see the subagent's final summary, never its raw actions, intermediate steps, or exact generated responses. "+
			"summary_instructions tells the subagent exactly what to extract into that summary for you — you must formulate it carefully, since anything you don't ask for will likely be missing from what you receive back. "+
			"Be specific: name the concrete facts, decisions, file paths, values, or conclusions you need, and the level of detail and format you expect (e.g. a short verdict vs. a full report). "+
			"A vague or generic summary_instructions (e.g. 'summarize what you did') will likely give you an incomplete or unusable result.",
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
				"type":        "array",
				"description": "Required for action='call'. List of one or more tasks to submit to subagents. " +
					"Every task must identify exactly one agent, contain one or more prompts, and a summary_instructions string. Ignored for action='list'.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent": map[string]any{
							"type":        "string",
							"description": "Name of the subagent that should run this task.",
						},
						"prompts": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
							"description": "One or more prompts (steps) to send to the selected subagent in order. Prompts do not reset the chat and can/should depend on each other!",
						},
						"summary_instructions": map[string]any{
							"type":        "string",
							"description": "Precise instructions telling the subagent what to include in its final summary to you. " +
								"You will only see this summary, not the subagent's raw work, so name exactly the facts, decisions, file paths, values, or conclusions you need, plus the expected level of detail and format.",
						},
					},
					"required":             []string{"agent", "prompts", "summary_instructions"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"action", "tasks"},
		"additionalProperties": false,
	}
}

type subagentCallTask struct {
	Agent               string   `json:"agent"`
	Prompts               []string `json:"prompts"`
	SummaryInstructions string   `json:"summary_instructions"`
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

		if len(task.Prompts) == 0 {
			pending = append(pending, pendingCall{
				agentName: task.Agent,
				err:       fmt.Errorf("task for agent %q must contain at least 1 prompt", task.Agent),
			})
			continue
		}

		summaryText := strings.TrimSpace(task.SummaryInstructions)
		if summaryText == "" {
			pending = append(pending, pendingCall{
				agentName: task.Agent,
				err:       fmt.Errorf("task for agent %q must contain non-empty summary_instructions", task.Agent),
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

		prompts := make([]handles.Prompt, 0, len(task.Prompts)+1)

		for _, prompt := range task.Prompts {
			if strings.TrimSpace(prompt) == "" {
				pending = append(pending, pendingCall{
					agentName: task.Agent,
					err:       fmt.Errorf("task for agent %q contains an empty prompt", task.Agent),
				})
				continue
			}

			prompts = append(prompts, handles.Prompt{Text: prompt})
		}

		if len(prompts) == 0 {
			continue
		}

		prompts = append(prompts, handles.Prompt{Text: summaryText})

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
