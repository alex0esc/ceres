package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/pkg/handles"
)


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

			var runCtx context.Context
			var cancel context.CancelFunc
			if t.Timeout > 0 {
				runCtx, cancel = context.WithTimeout(t.ParentCtx, t.Timeout)
			} else {
				runCtx, cancel = context.WithCancel(t.ParentCtx)
			}

			agent.currentTask = t    
			agent.mutex.Unlock()

			switch t.Tasktype {
			case task.TaskTypeCompress:
				err := agent.Client.CompressHistory(runCtx)
				t.ResultCh <- task.TaskResult{Response: nil, Err: err}
			case task.TaskTypeClear:
				agent.Client.ClearHistory()
				t.ResultCh <- task.TaskResult{Response: nil, Err: nil}
			case task.TaskTypeAsk, task.TaskTypeClearAsk:
				if t.Tasktype == task.TaskTypeClearAsk {
					agent.Client.ClearHistory()
				}
				var fullResp *history.History = &history.History{}
				for _, promt := range t.Prompts {
					resp, err, interrupted := agent.Client.AskStream(runCtx, promt, agent)
					if interrupted {
						t.ResultCh <- task.TaskResult{Response: fullResp, Err: nil, Interrupted: true }
						goto Done
					}
					if err != nil {
						t.ResultCh <- task.TaskResult{Response: fullResp, Err: err, Interrupted: false }
						goto Done
					}

					fullResp.Append(*resp)				
				}
				t.ResultCh <- task.TaskResult{Response: fullResp, Err: nil, Interrupted: false}
			}
			Done:
			cancel()
			agent.currentTask = nil
		}
	}
}

// SubmitTask enqueues a new task and returns a channel that will receive its result.
func (agent *Agent) SubmitTask(tsk task.Task) <-chan task.TaskResult {
	resultCh := make(chan task.TaskResult, 1)

	if inChain(tsk.ParentCtx, agent.name) {
		resultCh <- task.TaskResult{
			Err: fmt.Errorf("agent cycle detected: agent %q calls itself (directly or indirectly)", agent.name),
		}
		return resultCh
	}

	tsk.ParentCtx = withAgent(tsk.ParentCtx, agent.name)
	
	tsk.ResultCh = resultCh

	agent.mutex.Lock()
	if agent.state == handles.AgentStateStopped {
		agent.mutex.Unlock()
		resultCh <- task.TaskResult{Err: errors.New("agent is stopped")}
		return resultCh
	}
	agent.queue = append(agent.queue, &tsk)
	agent.mutex.Unlock()

	// wake up the worker; non-blocking, since the worker drains the whole queue once woken
	select {
	case agent.workCh <- struct{}{}:
	default:
	}
	return resultCh
}

type callChainKey struct{}

// withAgent appends the current agent name to the call chain stored in the context.
func withAgent(ctx context.Context, name string) context.Context {
	chain, _ := ctx.Value(callChainKey{}).([]string)
	newChain := append(append([]string{}, chain...), name) // copy, don't mutate!
	return context.WithValue(ctx, callChainKey{}, newChain)
}

// inChain checks whether an agent name is already present in the current call chain.
func inChain(ctx context.Context, name string) bool {
	chain, _ := ctx.Value(callChainKey{}).([]string)
	return slices.Contains(chain, name)
}
