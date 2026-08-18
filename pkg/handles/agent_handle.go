package handles


type AgentState int

const (
	AgentStateIdle AgentState = iota
	AgentStateBusy
	AgentStateStopped
)

func (s AgentState) String() string {
	switch s {
	case AgentStateIdle:
		return "idle"
	case AgentStateBusy:
		return "busy"
	case AgentStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

//result returned by submitTask
type TaskResult struct {
	Response string
	Err      error
}

type ClientHandle interface {
	AppendImage(base64Image string, mimeType string, promt string)
}


// AgentHandle describes everything a tool needs to know about an agent.
type AgentHandle interface {
	Name() string
	Description() string
	State() AgentState
	SubmitTask(task *Task) <-chan TaskResult
	ClientHandle() ClientHandle
	CurrentTask() *Task 
}
