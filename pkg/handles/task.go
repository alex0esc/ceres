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



type ImageInput struct {
	Base64Image string
	MimeType    string
}

type Prompt struct {
	Text string
	Images []ImageInput
}

// task represents a single queued unit of work
type Task struct {
	ParentCtx    context.Context
	Timeout      time.Duration
	Prompts      []Prompt
	Tasktype     TaskType
	ResultCh     chan TaskResult
}


func TaskAskSimple(prompt string, timeout time.Duration) Task {
	return Task{Prompts: []Prompt{ { Text: prompt, Images: nil }}, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeAsk}
}

func TaskAskSingle(promts Prompt, timeout time.Duration) Task {
	return Task{Prompts: []Prompt{ promts }, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeAsk}
}

func TaskClearAskMultiple(promts []Prompt, timeout time.Duration) Task {
	return Task{Prompts: promts, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClearAsk}
}

func TaskClear(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClear}
}

func TaskCompression(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeCompress}
}
