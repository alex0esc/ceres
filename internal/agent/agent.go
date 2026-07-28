package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/alex0esc/ceres/internal/openai"
	"github.com/alex0esc/ceres/pkg/handles"
) 

type Agent struct {
	name   string
	description string
	Client *openai.Client
	busy   chan struct{} //only one task at a time
	subagent bool
}

func NewAgent(name, description string, client *openai.Client, subagent bool) *Agent {
	return &Agent{
		name:   name,
		Client: client,
		description: description,
		busy:   make(chan struct{}, 1),
		subagent: subagent,
	}
}


//struct for context, only used es unique value
type callChainKey struct{}

// WithAgent appends the current agent name to the chain in the context.
func withAgent(ctx context.Context, name string) context.Context {
	chain, _ := ctx.Value(callChainKey{}).([]string)
	newChain := append(append([]string{}, chain...), name) // copy, don't mutate!
	return context.WithValue(ctx, callChainKey{}, newChain)
}

// inChain checks whether an agent name is already present in the current chain.
func inChain(ctx context.Context, name string) bool {
	chain, _ := ctx.Value(callChainKey{}).([]string)
	return slices.Contains(chain, name)
}



func (agent *Agent) SubmitTask(ctx context.Context, task string, clearHistory bool, timeout time.Duration) <-chan handles.TaskResult {
	resultCh := make(chan handles.TaskResult, 1)

	go func() {
		if inChain(ctx, agent.name) {
			resultCh <- handles.TaskResult{
				Err: fmt.Errorf("%w: agent %q calls itself (directly or indirectly)",
					errors.New("agent cycle detected"), agent.name)}
			return
		}

		agent.busy <- struct{}{}
		defer func() { <-agent.busy }()

		// Important: extend the chain with THIS agent before spawning subtasks
		ctx = withAgent(ctx, agent.name)

		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		if clearHistory {
			agent.Client.ClearHistory()
		}

		resp, err := agent.Client.AskStream(ctx, task)
		resultCh <- handles.TaskResult{Response: resp, Err: err}
	}()
	return resultCh
}


func (agent *Agent) Name() string {
	return agent.name
}

func (agent *Agent) Description() string {
	return agent.description
}

func (agent *Agent) Busy() bool {
	return len(agent.busy) == 1
}

func (agent *Agent) IsSubagent() bool {
	return agent.subagent
}
