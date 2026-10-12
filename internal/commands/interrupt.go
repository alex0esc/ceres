package commands

import (
	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)


func NewInterruptCommand() command.Command {
	return command.Command{
		Name:        "interrupt",
		Description: "Interrupt the running task of the selected agent.",
		Handler:     handleInterrupt,
	}
}

func handleInterrupt(agnt handles.AgentHandle, args []string) string {
	if len(args) > 0 {
		return command.NoArgs("interrupt")
	}
	agnt.(*agent.Agent).Client.Interrupt()
	return "*Agent has been interrupted.*"
}
