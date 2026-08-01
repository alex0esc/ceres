package server

import (
	"fmt"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/openai"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/platforms"
	"github.com/alex0esc/ceres/pkg/tools"
	"github.com/robfig/cron/v3"
)


type Server struct {
	endpoints map[string]openai.Endpoint	
	agents map[string]*agent.Agent
	croneJobs *cron.Cron
	config *config.Config
}



// creates the server and loads configs
func NewServer() (*Server, error) {
	// configs for tool call and platform settings
	cfg, err := config.New(ServerConfigPath)
	if err != nil {
		return nil, fmt.Errorf("error loading server config: %v", err)
	}
	tools.LoadToolConfig(ToolConfigPath)
	platforms.LoadPlatformConfig(PlatformConfigPath)

	// load endpoints configurations
	endpoints, err := openai.LoadEndpointsFromConfig(EndpointsConfigPath)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", EndpointsConfigPath, err)
	}	
	// load agent files
	agents, err := agent.LoadAgentsFromDir(AgentsFolderPath, endpoints)
	if err != nil {
		return nil, fmt.Errorf("error loading agent: %v", err)
	}
	
	return &Server {
		endpoints: endpoints,
		agents: agents,
		croneJobs: cron.New(),
		config: cfg,
	}, nil
}


func (server *Server) GetAgent(name string) *agent.Agent {
	agnt, ok := server.agents[name]
	if !ok {	
		panic(fmt.Sprintf("unknown agent %s", name))
	}
	return agnt
}


func (server *Server) GetAgentList() []*agent.Agent {
    list := make([]*agent.Agent, 0, len(server.agents))
    for _, agnt := range server.agents {
        list = append(list, agnt)
    }
    return list
}

