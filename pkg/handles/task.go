package handles

import (
	"context"
	"time"
)

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
	Prompts       []string
	Tasktype     TaskType
	ResultCh     chan TaskResult
}


func TaskAskSimple(promt string, timeout time.Duration) Task {
	return Task{Prompts: []string{ promt }, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeAsk}
}

func TaskClearAskMultiple(promts []string, timeout time.Duration) Task {
	return Task{Prompts: promts, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClearAsk}
}

func TaskClear(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClear}
}

func TaskCompression(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeCompress}
}
