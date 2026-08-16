package handles

import (
	"context"
	"time"
)


type AgentState int

const (
	AgentStateIdle AgentState = iota
	AgentStateBusy
	AgentStateStopped
)

func (s AgentState) String() string {
	switch s {
	case AgentStateIdle:
		return "idle"
	case AgentStateBusy:
		return "busy"
	case AgentStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}


// type of task that should be done
type TaskType int

const(
	TaskTypeAsk	= iota
	TaskTypeClear
	TaskTypeClearAsk
	TaskTypeCompress
)


// task represents a single queued unit of work
type Task struct {
	ParentCtx    context.Context
	Timeout      time.Duration
	Prompt       string
	Tasktype     TaskType
	ResultCh     chan TaskResult
	CheckList    map[string]bool
}

func (t *Task) CheckRemaining() []string {
	remaining := make([]string, 0, len(t.CheckList))
	for name, done := range t.CheckList {
		if !done {
			remaining = append(remaining, name)
		}
	}
	return remaining
}


func NewTaskAsk(promt string, timeout time.Duration) Task {
	return Task{Prompt: promt, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeAsk}
}

func NewTaskClearAsk(promt string, timeout time.Duration) Task {
	return Task{Prompt: promt, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClearAsk}
}

func NewTaskClear(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClear}
}

func NewTaskCompression(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeCompress}
}



// AgentHandle describes everything a tool needs to know about an agent.
type AgentHandle interface {
	Name() string
	Description() string
	State() AgentState
	SubmitTask(task Task) <-chan TaskResult
	CheckListSet(checkList map[string]bool)
	CheckListPop(name string) error
}

//result returned by submitTask
type TaskResult struct {
	Response string
	Err      error
}


