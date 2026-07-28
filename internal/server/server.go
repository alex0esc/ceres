package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/cronejob"
	"github.com/alex0esc/ceres/internal/openai"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platforms"
	"github.com/alex0esc/ceres/pkg/tools"
	"github.com/robfig/cron/v3"
)


type Server struct {
	endpoints map[string]openai.Endpoint	
	agents map[string]*agent.Agent
	croneJobs *cron.Cron

}

// initializes the server and load configs
func NewServer() (*Server, error) {
	// configs for tool call and platform settings
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

	LoadSubagentTools(agents)
	LoadPlatforms(agents)
	
	return &Server {
		endpoints: endpoints,
		agents: agents,
		croneJobs: cron.New(),
	}, nil
}


// starts all chrone jobs 
func (sv *Server) StartCroneJobs() error {
	jobs, err := cronjob.LoadCronJobsFromFile(CronJobsConfigPath)
	if err != nil {
		return err
	}
	return cronjob.RegisterCronJobs(sv.croneJobs, jobs, func(job cronjob.CronJobConfig) error {
		_, ok := sv.agents[job.AgentName]
		if !ok {
			return fmt.Errorf("agent %s does not exist", job.AgentName)
		}
		_, err := time.ParseDuration(job.Timeout)
		if err != nil {
			return err
		}
		return nil
	},
	func(job cronjob.CronJobConfig) {
		timeout, _ := time.ParseDuration(job.Timeout)
		res := <- sv.agents[job.AgentName].SubmitTask(context.Background(), job.Prompt, true, timeout)
		if res.Err != nil {
			log.Printf("error while executing chrone job: %v", res.Err)
		}

	})
}



// initilaizes the subagnent tools with the right agent references
func LoadSubagentTools(agents map[string]*agent.Agent) {
	var subagentList []handles.AgentHandle   
	for _, agent := range agents {
		if(agent.IsSubagent()) {
			subagentList = append(subagentList, agent)
		}
	}

	subagntListTool := tools.Get("subagent_list")
	concrete, ok := subagntListTool.(*tools.SubagentList)
	if ok {
		concrete.SetSubagentList(subagentList)
	}

	subagntCallTool := tools.Get("subagent_call")
	concrete1, ok1 := subagntCallTool.(*tools.SubagentCall)
	if ok1 {
		concrete1.SetSubagentList(subagentList)
	}
}

func LoadPlatforms(agents map[string]*agent.Agent) {
	for _, plat := range platforms.All() {
		agnt, ok := agents[plat.AgentName()]
		if !ok {
			panic(fmt.Sprintf("could not load platform: agent with name %s does not exist", agnt.Name()))
		}
		go func() {
			plat.Listen(agnt)
		}()
	}
}


func (server *Server) GetAgent(name string) *agent.Agent {
	agnt, ok := server.agents[name]
	if ok {
		return agnt
	} else {
		return nil
	}
}


func (server *Server) GetAgentList() []*agent.Agent {
    list := make([]*agent.Agent, 0, len(server.agents))
    for _, agnt := range server.agents {
        list = append(list, agnt)
    }
    return list
}
