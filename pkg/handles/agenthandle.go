package handles

import (
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/stream"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/config"
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

// ClientHandle is the part of an agent's client that platforms and tools may
// use. Streaming is exposed through neutral stream types so callers do not
// need to import the inference implementation.
type ClientHandle interface {
	AppendUserPrompt(prompt task.Prompt)

	// Subscribe registers a live subscriber without taking a snapshot. New
	// consumers should normally use SnapshotAndSubscribe instead.
	Subscribe(buffer int) stream.Stream
	// SnapshotAndSubscribe atomically captures the current state and registers
	// the subscription, so no token can be lost or delivered twice.
	SnapshotAndSubscribe(buffer int) (stream.Snapshot, stream.Stream)
	Snapshot() stream.Snapshot

	Interrupt()
	TotalTokens() int64
	IsCompressing() bool
	ReplaceHistory(history.History)
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
	SandboxContainer() string
	Config() *config.Config
}
