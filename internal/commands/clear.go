package commands

import (
	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)

func init() {
	command.Register(command.Command{
		Name:        "clr",
		Description: "Clear the chat history of the selected agent.",
		Handler:     handleClear,
	})
}

func handleClear(agnt handles.AgentHandle, args []string) string {
	if len(args) > 0 {
		return "*The clear command does not take arguments!*"
	}
	agnt.(*agent.Agent).Client.ClearHistory()
	return "*Agent history has been reset.*"
}
