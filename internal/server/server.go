package server

import (
	"fmt"
	"log"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/robfig/cron/v3"
)


type Server struct {
	endpoints map[string]inference.Endpoint	
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

	tool.LoadToolConfig(ToolConfigPath)
	if err != nil {
		return nil, fmt.Errorf("error loading tool config: %v", err)
	}

	err = tool.RegisterExternal()
	if err != nil {
		return nil, fmt.Errorf("error registering external tool: %v", err)
	}

	err = platform.LoadPlatformConfig(PlatformConfigPath)
	if err != nil {
		return nil, fmt.Errorf("error loading platform config: %v", err)
	}

	// load endpoints configurations
	endpoints, err := inference.LoadEndpointsFromConfig(EndpointsConfigPath)
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
		log.Fatalf("unknown agent %s", name)
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


func (server *Server) GetConfig() *config.Config {
	if server.config == nil {	
		log.Fatal("server config is nil, initialize it first")
	}
	return server.config
}
