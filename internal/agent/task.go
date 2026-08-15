package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex0esc/ceres/pkg/handles"
)


// task represents a single queued unit of work
type task struct {
	parentCtx    context.Context
	timeout      time.Duration
	prompt       string
	tasktype     handles.TaskType
	resultCh     chan handles.TaskResult
}


// worker processes queued tasks one at a time, in FIFO order.
func (agent *Agent) worker() {
	for range agent.workCh {
		for {
			agent.mutex.Lock()
			if agent.state == handles.AgentStateStopped {
				agent.mutex.Unlock()
				break
			}
			if len(agent.queue) == 0 {
				agent.state = handles.AgentStateIdle
				agent.mutex.Unlock()
				break
			}
			t := agent.queue[0]
			agent.queue = agent.queue[1:]
			agent.state = handles.AgentStateBusy
			agent.mutex.Unlock()

			var runCtx context.Context
			var cancel context.CancelFunc
			if t.timeout > 0 {
				runCtx, cancel = context.WithTimeout(t.parentCtx, t.timeout)
			} else {
				runCtx, cancel = context.WithCancel(t.parentCtx)
			}

			switch t.tasktype {
			case handles.TaskTypeCompress:	
				err := agent.Client.CompressHistory(runCtx)
				t.resultCh <- handles.TaskResult{Response: "", Err: err}
			case handles.TaskTypeClear:
				agent.Client.ClearHistory()
				t.resultCh <- handles.TaskResult{Response: "", Err: nil}
			case handles.TaskTypeAsk:
				resp, err := agent.Client.AskStream(runCtx, t.prompt, false)
				t.resultCh <- handles.TaskResult{Response: resp, Err: err}
			}			
			cancel() // local var, purely resource cleanup
		}
	}
}


// SubmitTask enqueues a new task and returns a channel that will receive its result.
func (agent *Agent) SubmitTask(ctx context.Context, prompt string, taskType handles.TaskType, timeout time.Duration) <-chan handles.TaskResult {
	resultCh := make(chan handles.TaskResult, 1)

	if inChain(ctx, agent.name) {
		resultCh <- handles.TaskResult{
			Err: fmt.Errorf("agent cycle detected: agent %q calls itself (directly or indirectly)", agent.name),
		}
		return resultCh
	}

	parentCtx := withAgent(ctx, agent.name)

	t := &task{
		parentCtx:    parentCtx,
		timeout:      timeout,
		prompt:       prompt,
		tasktype:     taskType,
		resultCh:     resultCh,
	}

	agent.mutex.Lock()
	if agent.state == handles.AgentStateStopped {
		agent.mutex.Unlock()
		resultCh <- handles.TaskResult{Err: errors.New("agent is stopped")}
		return resultCh
	}
	agent.queue = append(agent.queue, t)
	agent.mutex.Unlock()

	// wake up the worker; non-blocking, since the worker drains the whole queue once woken
	select {
	case agent.workCh <- struct{}{}:
	default:
	}
	return resultCh
}
