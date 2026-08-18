package handles

import (
	"context"
	"fmt"
	"maps"
	"sync"
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
	Prompt       string
	Tasktype     TaskType
	ResultCh     chan TaskResult
	CheckList    map[string]bool
	mutex        sync.Mutex
}


func TaskAsk(promt string, timeout time.Duration) Task {
	return Task{Prompt: promt, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeAsk}
}

func TaskClearAsk(promt string, timeout time.Duration) Task {
	return Task{Prompt: promt, Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClearAsk}
}

func TaskClear(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeClear}
}

func TaskCompression(timeout time.Duration) Task {
	return Task{Timeout: timeout, ParentCtx: context.Background(), Tasktype: TaskTypeCompress}
}


func (t *Task) CheckListShow() map[string]bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.CheckList == nil {
		return nil
	}

	checklist := make(map[string]bool, len(t.CheckList))
	maps.Copy(checklist, t.CheckList)

	return checklist
}

func (t *Task) CheckListSet(checkList map[string]bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.CheckList = make(map[string]bool, len(checkList))
	maps.Copy(t.CheckList, checkList)
}

func (t *Task) CheckListDone(name string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.CheckList == nil {
		return fmt.Errorf("checklist does not exist")
	}

	if _, exists := t.CheckList[name]; !exists {
		return fmt.Errorf("the item with name %s is not part of the checklist", name)
	}

	t.CheckList[name] = true
	return nil
}


func (t *Task) CheckRemaining() []string {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	remaining := make([]string, 0, len(t.CheckList))
	for name, done := range t.CheckList {
		if !done {
			remaining = append(remaining, name)
		}
	}

	return remaining
}
