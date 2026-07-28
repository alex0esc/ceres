package handles

import (
	"context"
	"time"
)


// AgentHandle describes everything a tool needs to know about an agent.
type AgentHandle interface {
	Name() string
	Description() string
	Busy() bool
	SubmitTask(ctx context.Context, task string, clearHistory bool, timeout time.Duration) <-chan TaskResult
}

//result returned by submitTask
type TaskResult struct {
	Response string
	Err      error
}


