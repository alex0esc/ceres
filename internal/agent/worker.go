package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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
			case handles.TaskTypeCompress:
				err := agent.Client.CompressHistory(runCtx)
				t.ResultCh <- handles.TaskResult{Response: "", Err: err}
			case handles.TaskTypeClear:
				agent.Client.ClearHistory()
				t.ResultCh <- handles.TaskResult{Response: "", Err: nil}
			case handles.TaskTypeAsk:
				var fullResp strings.Builder
				resp, err := agent.Client.AskStream(runCtx, t.Prompt, false, agent)
				if err != nil {
					t.ResultCh <- handles.TaskResult{Response: resp, Err: err}
					goto Done
				}
				fullResp.WriteString(resp)
				for {
					if err = runCtx.Err(); err != nil {
						t.ResultCh <- handles.TaskResult{Response: fullResp.String(), Err: err}
						goto Done
					}
					agent.mutex.Lock()
					remaining := t.CheckRemaining()
					agent.mutex.Unlock()
					if len(remaining) <= 0 {
						break	
					}
					resp, err = agent.Client.AskStream(runCtx,
						fmt.Sprintf("Checklist is not empty, remaining tasks are: %s", quoteJoin(remaining)), true, agent)
					if err != nil {
						t.ResultCh <- handles.TaskResult{Response: fullResp.String(), Err: err}
						goto Done
					}
				}
				t.ResultCh <- handles.TaskResult{Response: fullResp.String(), Err: err}
			}

			Done:
			cancel()
			agent.mutex.Lock()
			agent.currentTask = nil
			agent.mutex.Unlock()
		}
	}
}

// SubmitTask enqueues a new task and returns a channel that will receive its result.
func (agent *Agent) SubmitTask(task handles.Task) <-chan handles.TaskResult {
	resultCh := make(chan handles.TaskResult, 1)

	if inChain(task.ParentCtx, agent.name) {
		resultCh <- handles.TaskResult{
			Err: fmt.Errorf("agent cycle detected: agent %q calls itself (directly or indirectly)", agent.name),
		}
		return resultCh
	}

	task.ParentCtx = withAgent(task.ParentCtx, agent.name)
	
	task.ResultCh = resultCh

	agent.mutex.Lock()
	if agent.state == handles.AgentStateStopped {
		agent.mutex.Unlock()
		resultCh <- handles.TaskResult{Err: errors.New("agent is stopped")}
		return resultCh
	}
	agent.queue = append(agent.queue, &task)
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

func quoteJoin(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}
