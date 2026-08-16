package commands

import (
	"log"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)

func init() {
	command.Register(command.Command{
		Name:        "cmp",
		Description: "Compress the chat history of the selected agent.",
		Handler:     handleCompress,
	})
}

func handleCompress(agnt handles.AgentHandle, args []string) string {
	if len(args) > 0 {
		return "The compress command does not take arguments!"
	}

	result := agnt.(*agent.Agent).SubmitTask(handles.NewTaskCompression(20 * time.Minute))
	go func() {
		res := <- result		
		if res.Err != nil {
			log.Printf("Error while after submitting compress: %v", res.Err)
		}
	}();

	return "*Compress task has been submitted to agent queue.*"
}
