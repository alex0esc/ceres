package app

import (
	"fmt"
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/commands"
	_ "github.com/alex0esc/ceres/internal/commands"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/internal/platforms"
	_ "github.com/alex0esc/ceres/internal/platforms"
	"github.com/robfig/cron/v3"

	"github.com/alex0esc/ceres/internal/cronejob"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
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

	registerInternalPlatforms()
	err = platform.RegisterExternal()
	if err != nil {
		return fmt.Errorf("error registering external platform: %v", err)
	}

	initPlatforms()

	for _, agent := range agents {
		agent.Start()
	}

	err = startCroneJobs()
	if err != nil {
		return err
	}

	registerInternalCommands()
	err = command.RegisterExternal()
	if err != nil {
		return err
	}

	return nil
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
	tool.ClearRegistry()
	platform.ClearRegistry()
	command.ClearRegistry()
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

	err = platform.LoadPlatformConfig(PlatformConfigPath)
	if err != nil {
		return fmt.Errorf("error loading platform config: %v", err)
	}

	endpoints, err = inference.LoadEndpointsFromConfig(EndpointsConfigPath)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", EndpointsConfigPath, err)
	}	
	
	registerInternalTools()
	err = tool.RegisterExternal()
	if err != nil {
		return fmt.Errorf("error registering external tool: %v", err)
	}

	agents, err = agent.LoadAgentsFromDir(AgentsFolderPath, endpoints)
	if err != nil {
		return fmt.Errorf("error loading agent: %v", err)
	}	
	return nil
}


func registerInternalTools() {
	tool.Register(tools.NewBashTool())
	tool.Register(tools.NewDiscordTool())
	tool.Register(tools.NewExecuteCodeTool())
	tool.Register(tools.NewFileEditTool())
	tool.Register(tools.NewFileReadTool())
	tool.Register(tools.NewGetTimeTool())
	tool.Register(tools.NewSearxngTool())
	tool.Register(tools.NewWebExtractTool())
	tool.Register(tools.NewSkillTool())
	tool.Register(tools.NewSubagentTool())
	tool.Register(tools.NewViewImageTool())
}


func registerInternalPlatforms() {
	platform.Register(platforms.NewDiscord())
}


func registerInternalCommands() {
	command.Register(commands.NewHelpCommand())
	command.Register(commands.NewClearCommand())
	command.Register(commands.NewCompressCommand())
	command.Register(commands.NewInterruptCommand())
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
		task := handles.TaskClearAskMultiple(job.Prompts, timeout)
		res := <- agents[job.AgentName].SubmitTask(&task)
		if res.Err != nil {
			log.Printf("error while executing chrone job: %v", res.Err)
		}

	})
}
