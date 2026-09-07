package app

import (
	"fmt"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/commands"
	_ "github.com/alex0esc/ceres/internal/commands"
	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/internal/platforms"
	_ "github.com/alex0esc/ceres/internal/platforms"
	"github.com/robfig/cron/v3"

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

	registerInternalCommands()
	err = command.RegisterExternal()
	if err != nil {
		return err
	}

	cronLib.Start()

	return nil
}


func Shutdown() {
	for _, agnt := range agents {
		agnt.Stop()
	}
	for _, plat := range config.ReadEntry(cfg, "active_platforms", []string{}) {
		platform.Get(plat).StopListen()
	}
	ctx := cronLib.Stop()
	<-ctx.Done()
	tools.CloseDockerClient()
	endpoints = nil
	agents = nil
	cronLib = nil
	cfg = nil
	tool.ClearRegistry()
	platform.ClearRegistry()
	command.ClearRegistry()
}


func loadConfigs() error {
	var err error
	cfg, err = config.New(constants.AppConfigPath)
	if err != nil {
		return fmt.Errorf("error loading server config: %v", err)
	}

	err = tool.LoadToolConfig()
	if err != nil {
		return fmt.Errorf("error loading tool config: %v", err)
	}

	err = platform.LoadPlatformConfig()
	if err != nil {
		return fmt.Errorf("error loading platform config: %v", err)
	}

	endpoints, err = inference.LoadEndpointsFromConfig()
	if err != nil {
		return fmt.Errorf("error reading endpoints config: %v", err)
	}	
	
	registerInternalTools()
	err = tool.RegisterExternal()
	if err != nil {
		return fmt.Errorf("error registering external tool: %v", err)
	}


	zone, err := time.LoadLocation(config.ReadEntry(tool.GetToolConfig(), "timezone", "Local"))
	if err != nil {
		return fmt.Errorf("invalid timezone in toolconfig")
	}
	cronLib = cron.New(cron.WithLocation(zone))
	agents, err = agent.LoadAgentsFromDir(endpoints, cronLib)
	if err != nil {
		return fmt.Errorf("error loading agents: %v", err)
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
	tool.Register(tools.NewMemoryReadTool())
	tool.Register(tools.NewMemoryEditTool())
	tool.Register(tools.NewSubagentTool())
	tool.Register(tools.NewViewImageTool())
	tool.Register(tools.NewWakeupTool())
}


func registerInternalPlatforms() {
	platform.Register(platforms.NewDiscord())
}


func registerInternalCommands() {
	command.Register(commands.NewHelpCommand())
	command.Register(commands.NewClearCommand())
	command.Register(commands.NewCompressCommand())
	command.Register(commands.NewInterruptCommand())
	command.Register(commands.NewWakeupCommand())
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

