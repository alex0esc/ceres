package agent

import (
	"errors"
	"fmt"
	"sync"

	"github.com/alex0esc/ceres/internal/inference"
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
	queue         []*handles.Task
	state         handles.AgentState
	workCh        chan struct{} // signals that there is work to do

	currentTask   *handles.Task

	wakeups       map[string]*wakeup.WakeUp
	cronLib      *cron.Cron 
}

func NewAgent(name, description string, client *inference.Client, subagent bool, cronLib *cron.Cron) *Agent {
	return &Agent{
		name:        name,
		description: description,
		Client:      client,
		subagent:    subagent,
		state:       handles.AgentStateStopped,
		workCh:      nil,
		cronLib:    cronLib,
	}
}


// Resume allows new tasks to be submitted and processed again
func (agent *Agent) Start() error {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	if agent.state != handles.AgentStateStopped {
		return fmt.Errorf("agent already running")
	}
	agent.workCh = make(chan struct{}, 1)
	agent.state = handles.AgentStateIdle
	go agent.worker()

	// load and start wakeups
	wakeups, err := wakeup.LoadWakeupsForAgent(agent)
	if err != nil {
		return err
	}

	agent.wakeups = wakeups

	for _, wu := range wakeups {
		wu.Start(agent.cronLib)
	}

	return nil
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
	wakeups := agent.wakeups
	agent.mutex.Unlock()

	for _, wu := range wakeups {
		wu.Stop()
	}

	agent.Client.Interrupt() // potentially slow, runs outside the lock

	for _, t := range pending {
		t.ResultCh <- handles.TaskResult{Err: errors.New("task cancelled: agent stopped")}
	}

	if ch != nil {
		close(ch)
	}
}

func (agent *Agent) Name() string        { return agent.name }
func (agent *Agent) Description() string { return agent.description }
func (agent *Agent) IsSubagent() bool    { return agent.subagent }
func (agent *Agent) ClientHandle() handles.ClientHandle { return agent.Client }


func (agent *Agent) CurrentTask() *handles.Task {
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


func (agent *Agent) ListWakeups() []handles.WakeupHandle {
	agent.mutex.Lock()
	result := make([]handles.WakeupHandle, 0, len(agent.wakeups))

	for _, wu := range agent.wakeups {
		result = append(result, wu)
	}
	agent.mutex.Unlock()
	return result
}

func (agent *Agent) ExecuteWakeup(name string) bool {
	agent.mutex.Lock()
	wu, ok := agent.wakeups[name]
	if !ok {
		agent.mutex.Unlock()
		return false
	}
	agent.mutex.Unlock()
	wu.Execute()
	return true
}


func (agent *Agent) RemoveWakeup(name string) error {
	agent.mutex.Lock()
	wu, ok := agent.wakeups[name]
	if !ok {
		agent.mutex.Unlock()
		return fmt.Errorf("wakeup %s does not exist", name)
	}
	delete(agent.wakeups, name)
	agent.mutex.Unlock()

	wu.Stop()
	return wu.Delete()
}

func (agent *Agent) AddWakeup(wh handles.WakeupHandle) error {
	agent.mutex.Lock()
	_, ok := agent.wakeups[wh.Name()]
	if ok {
		return fmt.Errorf("wakeup with name %s already exists", wh.Name())
	}

	wu := wh.(*wakeup.WakeUp)
	if err := wu.Start(agent.cronLib); err != nil {
		return err
	}
	if err := wu.Save(); err != nil {
		wu.Stop()
		return err
	}

	agent.wakeups[wh.Name()] = wu  
	agent.mutex.Unlock()
	return nil
}

func (agent *Agent) GetWakeup(name string) handles.WakeupHandle {
	agent.mutex.Lock()
	wu, ok := agent.wakeups[name]
	agent.mutex.Unlock()
	if !ok {
		return nil
	}
	return wu
}
