package agent

import (
	"errors"
	"sync"

	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/pkg/handles"
)


type Agent struct {
	name        string
	description string
	Client      *inference.Client
	subagent    bool

	mutex         sync.Mutex
	queue         []*handles.Task
	state         handles.AgentState
	workCh        chan struct{} // signals that there is work to do

	//for checkList
	currentTask   *handles.Task
}

func NewAgent(name, description string, client *inference.Client, subagent bool) *Agent {
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
		t.ResultCh <- handles.TaskResult{Err: errors.New("task cancelled: agent stopped")}
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
func (agent *Agent) ClientHandle() handles.ClientHandle { return agent.Client }
func (agent *Agent) CurrentTask() *handles.Task        { return agent.currentTask }



