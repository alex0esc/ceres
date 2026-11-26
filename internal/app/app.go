package app

import (
	"log"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/robfig/cron/v3"
)


var endpoints map[string]inference.Endpoint	
var agents map[string]*agent.Agent
var cronLib *cron.Cron



func GetAgent(name string) *agent.Agent {
	agnt, ok := agents[name]
	if !ok {	
		log.Fatalf("unknown agent %s", name)
	}
	return agnt
}


func GetAgentList() []*agent.Agent {
    list := make([]*agent.Agent, 0, len(agents))
    for _, agnt := range agents {
        list = append(list, agnt)
    }
    return list
}

func GetHandleMap() map[string]handles.AgentHandle {
	agent_handles := map[string]handles.AgentHandle{}

	for name, agent := range agents {
		agent_handles[name] = agent
	}	

	return agent_handles
}
