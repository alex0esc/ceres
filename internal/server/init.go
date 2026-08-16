package server

import (
	"fmt"
	"log"
	"time"


	_ "github.com/alex0esc/ceres/internal/commands"
	_ "github.com/alex0esc/ceres/internal/platforms"

	"github.com/alex0esc/ceres/internal/cronejob"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/platform"
)

// initializes/starts necessary stuff
func (sv *Server) Initialize() error {
	sv.initSubagentTools()
	err := tools.InitDockerClient()
	if err != nil {
		return err
	}
	sv.initPlatforms()
	for _, agent := range sv.agents {
		agent.Start()
	}
	return sv.startCroneJobs()
}


func (sv *Server) Shutdown() {
	for _, agnt := range sv.agents {
		agnt.Stop()
	}
	for _, plat := range config.ReadEntry(sv.config, "active_platforms", []string{}) {
		platform.Get(plat).StopListen()
	}
	ctx := sv.croneJobs.Stop()
	<-ctx.Done()
}


// initilaizes the subagnent tools with the right agent references
func (sv *Server) initSubagentTools() {
	var subagentList map[string]handles.AgentHandle = make(map[string]handles.AgentHandle) 
	for _, agent := range sv.agents {
		if(agent.IsSubagent()) {
			subagentList[agent.Name()] = agent
 		}
	}
	tools.SetSubagents(subagentList)
}

func (sv *Server) initPlatforms() {
	for _, name := range config.ReadEntry(sv.config, "active_platforms", []string{}) {
		plat := platform.Get(name)	
		go plat.Listen(sv.GetAgent(plat.AgentName()))
	}
}

// starts all chrone jobs 
func (sv *Server) startCroneJobs() error {
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
		res := <- sv.agents[job.AgentName].SubmitTask(handles.NewTaskAsk(job.Prompt, timeout))
		if res.Err != nil {
			log.Printf("error while executing chrone job: %v", res.Err)
		}

	})
}
