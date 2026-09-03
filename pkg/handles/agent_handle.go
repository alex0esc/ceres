package handles

import "github.com/alex0esc/ceres/internal/history"

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

// result returned by submitTask
type TaskResult struct {
	Interrupted bool
	Response *history.History
	Err      error
}


type ClientHandle interface {
	AppendUserPrompt(prompt Prompt)
}

// AgentHandle describes everything a tool needs to know about an agent.
type AgentHandle interface {
	Name() string
	Description() string
	State() AgentState
	SubmitTask(task Task) <-chan TaskResult
	ClientHandle() ClientHandle
	CurrentTask() *Task
}
