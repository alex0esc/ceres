package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// ChecklistTool manages the agent's current checklist.
//
// Actions:
//   - show: shows the current checklist and whether each task is done.
//   - create: creates a new checklist, overwriting the existing one.
//   - done: marks a task as finished.
type ChecklistTool struct{}

func (t *ChecklistTool) Name() string {
	return "checklist"
}

func (t *ChecklistTool) Description() string {
	return "USAGE: This tool is meant to be used for longer multi step tasks, " +
		"to ensure the task is done completely without missing anything. " +
		"Only after marking every task as done on a checklist a new one can be created. " +
		"Checklists have to be fninished, they are binding. " +
		"If one task unfinished there will be a system message saying there are still tasks remaning." +
		"action='show': shows the current checklist and which tasks are done. " +
		"action='create': creates a new checklist, only works if no checklist exists. " +
		"action='done': marks a task in the checklist as finished."
}

func (t *ChecklistTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"show", "create", "done"},
				"description": "Which checklist operation to perform.",
			},
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Required for action='create': the list of tasks for the new checklist. " +
					"All tasks start as unfinished.",
			},
			"task": map[string]any{
				"type": "string",
				"description": "Required for action='done': the exact name of the task to mark as finished.",
			},
		},
		"required":             []string{"action", "tasks", "task"},
		"additionalProperties": false,
	}
}

type checklistToolArgs struct {
	Action string   `json:"action"`
	Tasks  []string `json:"tasks"`
	Task   string   `json:"task"`
}

func (t *ChecklistTool) Handler() tool.ToolHandler {
	return func(
		ctx context.Context,
		argumentsJSON string,
		handle handles.AgentHandle,
	) (string, error) {
		var args checklistToolArgs

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("checklist: invalid arguments: %w", err)
		}

		switch args.Action {
		case "show":
			return checklistShow(handle)

		case "create":
			return checklistCreate(handle, args.Tasks)

		case "done":
			return checklistDone(handle, args.Task)

		case "":
			return "", fmt.Errorf(
				"checklist: 'action' is required (must be 'show', 'create' or 'done')",
			)

		default:
			return "", fmt.Errorf(
				"checklist: unknown action %q (must be 'show', 'create' or 'done')",
				args.Action,
			)
		}
	}
}


func checklistShow(handle handles.AgentHandle) (string, error) {
	checklist := handle.CurrentTask().CheckListShow()

	if checklist == nil {
		checklist = map[string]bool{}
	}

	out, err := json.Marshal(map[string]any{
		"tasks": checklist,
	})
	if err != nil {
		return "", fmt.Errorf("checklist_show: failed to marshal result: %w", err)
	}

	return string(out), nil
}


func checklistCreate(handle handles.AgentHandle, tasks []string) (string, error) {
	if len(handle.CurrentTask().CheckRemaining()) > 0 {
		return "", fmt.Errorf("checklist_create: a checklist which is not finished already exists")
	}

	if len(tasks) == 0 {
		return "", fmt.Errorf("checklist_create: tasks must not be empty")
	}

	checklist := make(map[string]bool, len(tasks))

	for _, task := range tasks {
		if task == "" {
			return "", fmt.Errorf("checklist_create: task names must not be empty")
		}

		checklist[task] = false
	}

	handle.CurrentTask().CheckListSet(checklist)

	out, err := json.Marshal(map[string]any{
		"created": true,
		"tasks":   checklist,
	})
	if err != nil {
		return "", fmt.Errorf("checklist_create: failed to marshal result: %w", err)
	}

	return string(out), nil
}



// checklistDone marks an existing task as finished.
func checklistDone(handle handles.AgentHandle, task string) (string, error) {
	if task == "" {
		return "", fmt.Errorf("checklist_done: task must not be empty")
	}

	if err := handle.CurrentTask().CheckListDone(task); err != nil {
		return "", fmt.Errorf("checklist_done: failed to mark task %q as done: %w", task, err)
	}

	out, err := json.Marshal(map[string]any{
		"task":    task,
		"done":    true,
		"updated": true,
	})
	if err != nil {
		return "", fmt.Errorf("checklist_done: failed to marshal result: %w", err)
	}

	return string(out), nil
}

func init() {
	tool.Register(&ChecklistTool{})
}

