package commands

import (
	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)


func init() {
	command.Register(command.Command{
		Name:        "itr",
		Description: "Interrupt the selected agent.",
		Handler:     handleInterrupt,
	})
}

func handleInterrupt(agnt handles.AgentHandle, args []string) string {
	if len(args) > 0 {
		return "*The interrupt command does not take arguments!*"
	}
	agnt.(*agent.Agent).Client.Interrupt()
	return "*Agent has been interrupted.*"
}
