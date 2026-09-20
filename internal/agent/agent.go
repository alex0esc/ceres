package agent

import (
	"errors"
	"fmt"
	"sync"

	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/wakeup"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/robfig/cron/v3"
)


type Agent struct {
	name          string
	description   string
	Client        *inference.Client
	subagent      bool

	mutex         sync.Mutex
	queue         []*task.Task
	state         handles.AgentState
	workCh        chan struct{} // signals that there is work to do

	currentTask   *task.Task

	cronLib      *cron.Cron 
	wakeups       *wakeup.Manager
}

// NewAgent creates an agent together with its wakeup manager. The manager loads
// the agent's wakeups from the wakeup config (wakeup.DbOpen must have been
// called), gets the agent's SubmitTask function, and is started and stopped
// together with the agent.
func NewAgent(name, description string, client *inference.Client, subagent bool, cronLib *cron.Cron) (*Agent, error) {
	agent := &Agent{
		name:        name,
		description: description,
		Client:      client,
		subagent:    subagent,
		state:       handles.AgentStateStopped,
		workCh:      nil,
		cronLib:    cronLib,
	}

	wakeups, err := wakeup.NewManager(name, agent.SubmitTask, cronLib)
	if err != nil {
		return nil, err
	}
	agent.wakeups = wakeups

	return agent, nil
}


// Resume allows new tasks to be submitted and processed again
func (agent *Agent) Start() error {
	agent.mutex.Lock()
	if agent.state != handles.AgentStateStopped {
		agent.mutex.Unlock()
		return fmt.Errorf("agent already running")
	}
	agent.workCh = make(chan struct{}, 1)
	agent.state = handles.AgentStateIdle
	go agent.worker()
	agent.mutex.Unlock()

	// The agent takes tasks now, so its wakeups may fire.
	agent.wakeups.StartAll()
	return nil
}

// Stops the agents and interrupts any running task
func (agent *Agent) Stop() {
	// Stop the wakeups first, so none of them fires into an agent that is shutting down.
	agent.wakeups.StopAll()

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
		t.ResultCh <- task.TaskResult{Err: errors.New("task cancelled: agent stopped")}
	}

	if ch != nil {
		close(ch)
	}
}

func (agent *Agent) Name() string        { return agent.name }
func (agent *Agent) Description() string { return agent.description }
func (agent *Agent) IsSubagent() bool    { return agent.subagent }
func (agent *Agent) ClientHandle() handles.ClientHandle { return agent.Client }
func (agent *Agent) WakeupManager() *wakeup.Manager     { return agent.wakeups }


func (agent *Agent) CurrentTask() *task.Task {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	return agent.currentTask
}

func (agent *Agent) State() handles.AgentState {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	return agent.state
}

func (agent *Agent) ClearQueue() {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	agent.queue = nil
}
