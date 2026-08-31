package app

import (
	"fmt"
	"log"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/internal/cronjob"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/robfig/cron/v3"
)


var endpoints map[string]inference.Endpoint	
var agents map[string]*agent.Agent
var cronJobs map[string]*cronjob.CronJob
var cronLib *cron.Cron
var cfg *config.Config



func GetAgent(name string) *agent.Agent {
	agnt, ok := agents[name]
	if !ok {	
		panic(fmt.Sprintf("unknown agent %s", name))
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


func GetAppConfig() *config.Config {
	if cfg == nil {	
		log.Fatal("server config is nil, initialize it first")
	}
	return cfg
}
