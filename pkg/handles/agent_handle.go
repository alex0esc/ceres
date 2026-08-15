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


// AgentHandle describes everything a tool needs to know about an agent.
type AgentHandle interface {
	Name() string
	Description() string
	State() AgentState
	SubmitTask(ctx context.Context, task string, taskType TaskType, timeout time.Duration) <-chan TaskResult
}

//result returned by submitTask
type TaskResult struct {
	Response string
	Err      error
}


