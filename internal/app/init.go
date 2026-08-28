package app

import (
	"fmt"
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	_ "github.com/alex0esc/ceres/internal/commands"
	"github.com/alex0esc/ceres/internal/inference"
	_ "github.com/alex0esc/ceres/internal/platforms"
	"github.com/robfig/cron/v3"

	"github.com/alex0esc/ceres/internal/cronejob"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
	"github.com/alex0esc/ceres/pkg/tool"
)

// initializes necessary stuff and loads configs, starts agents
func  Start() error {
	var err error = loadConfigs()
	if err != nil {
		return err
	}

	tools.SetSkillDir(SkillFolderPath)
	initSubagentTool()
	err = tools.InitDockerClient()
	if err != nil {
		return err
	}

	initPlatforms()

	for _, agent := range agents {
		agent.Start()
	}
	return startCroneJobs()
}


func Shutdown() {
	for _, agnt := range agents {
		agnt.Stop()
	}
	for _, plat := range config.ReadEntry(cfg, "active_platforms", []string{}) {
		platform.Get(plat).StopListen()
	}
	ctx := croneJobs.Stop()
	<-ctx.Done()
	tools.CloseDockerClient()
	endpoints = nil
	agents = nil
	croneJobs = nil
	cfg = nil
}



func loadConfigs() error {
	var err error
	cfg, err = config.New(AppConfigPath)
	if err != nil {
		return fmt.Errorf("error loading server config: %v", err)
	}

	err = tool.LoadToolConfig(ToolConfigPath)
	if err != nil {
		return fmt.Errorf("error loading tool config: %v", err)
	}

	err = tool.RegisterExternal()
	if err != nil {
		return fmt.Errorf("error registering external tool: %v", err)
	}

	err = platform.LoadPlatformConfig(PlatformConfigPath)
	if err != nil {
		return fmt.Errorf("error loading platform config: %v", err)
	}

	endpoints, err = inference.LoadEndpointsFromConfig(EndpointsConfigPath)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", EndpointsConfigPath, err)
	}	

	agents, err = agent.LoadAgentsFromDir(AgentsFolderPath, endpoints)
	if err != nil {
		return fmt.Errorf("error loading agent: %v", err)
	}	
	return nil
}


// initilaizes the subagnent tools with the right agent references
func initSubagentTool() {
	var subagentList map[string]handles.AgentHandle = make(map[string]handles.AgentHandle) 
	for _, agent := range agents {
		if(agent.IsSubagent()) {
			subagentList[agent.Name()] = agent
 		}
	}
	tools.SetSubagents(subagentList)
}

// initializes platforms
func initPlatforms() {
	for _, name := range config.ReadEntry(cfg, "active_platforms", []string{}) {
		plat := platform.Get(name)	
		go plat.Listen(GetAgent(plat.AgentName()))
	}
}

// starts all chrone jobs 
func startCroneJobs() error {
	croneJobs = cron.New()

	jobs, err := cronjob.LoadCronJobsFromFile(CronJobsConfigPath)
	if err != nil {
		return err
	}
	return cronjob.RegisterCronJobs(croneJobs, jobs, func(job cronjob.CronJobConfig) error {
		_, ok := agents[job.AgentName]
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
		task := handles.TaskAsk(job.Prompt, timeout)
		res := <- agents[job.AgentName].SubmitTask(&task)
		if res.Err != nil {
			log.Printf("error while executing chrone job: %v", res.Err)
		}

	})
}
