package handles

import (
	"time"

	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/wakeup"
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
		return "Idle"
	case AgentStateBusy:
		return "Busy"
	case AgentStateStopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}


type ClientHandle interface {
	AppendUserPrompt(prompt task.Prompt)
}


type WakeupHandle interface {
	Name() string
	Description() string
	FireAt() *time.Time
	CronSpec() string
	Protected() bool
	Prompts() []string

}


// AgentHandle describes everything a tool needs to know about an agent.
type AgentHandle interface {
	Name() string
	Description() string
	State() AgentState
	SubmitTask(task task.Task) <-chan task.TaskResult
	ClientHandle() ClientHandle
	CurrentTask() *task.Task
	WakeupManager() *wakeup.Manager
}
