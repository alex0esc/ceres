package agent


import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/alex0esc/ceres/internal/openai"
	"github.com/alex0esc/ceres/pkg/handles"
)


type Agent struct {
	name        string
	description string
	Client      *openai.Client
	subagent    bool

	mutex         sync.Mutex
	queue         []*task
	state         handles.AgentState
	workCh        chan struct{} // signals that there is work to do
}

func NewAgent(name, description string, client *openai.Client, subagent bool) *Agent {
	return &Agent{
		name:        name,
		description: description,
		Client:      client,
		subagent:    subagent,
		state:       handles.AgentStateStopped,
		workCh:      nil,
	}
}


// Resume allows new tasks to be submitted and processed again
func (agent *Agent) Start() {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	if agent.state != handles.AgentStateStopped {
		return
	}
	agent.workCh = make(chan struct{}, 1)
	agent.state = handles.AgentStateIdle
	go agent.worker()
}

// Stops the agents and interrupts any running task
func (agent *Agent) Stop() {
	agent.mutex.Lock()
	if agent.state == handles.AgentStateStopped {
		agent.mutex.Unlock()
		return
	}
	agent.state = handles.AgentStateStopped
	ch := agent.workCh // capture before releasing the lock, so no one can swap it out from under us
	pending := agent.queue
	agent.queue = nil
	agent.mutex.Unlock()

	agent.Client.Interrupt() // potentially slow, runs outside the lock

	for _, t := range pending {
		t.resultCh <- handles.TaskResult{Err: errors.New("task cancelled: agent stopped")}
	}

	close(ch)
}

func (agent *Agent) State() handles.AgentState {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	return agent.state
}

func (agent *Agent) Name() string        { return agent.name }
func (agent *Agent) Description() string { return agent.description }
func (agent *Agent) IsSubagent() bool    { return agent.subagent }


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
