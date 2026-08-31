package commands

import (
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/internal/cronjob"
	"github.com/alex0esc/ceres/pkg/command"
	"github.com/alex0esc/ceres/pkg/handles"
)


func NewCronCommand(jobs map[string]*cronjob.CronJob) command.Command {
	return command.Command{
		Name:        "cron",
		Description: "Manage and run registered cron jobs.",
		Handler: func(agent handles.AgentHandle, args []string) string {
			return handleCron(jobs, agent, args)
		},
	}
}

func handleCron(jobs map[string]*cronjob.CronJob, _ handles.AgentHandle, args []string) string {
	if len(args) == 0 {
		return "Usage: `/cron list` or `/cron run <name>`"
	}

	subcommand := strings.ToLower(args[0])

	switch subcommand {
	case "list":
		if len(jobs) == 0 {
			return "No cron jobs registered."
		}

		var b strings.Builder
		b.WriteString("## Registered Cron Jobs\n\n")
		for name := range jobs {
			fmt.Fprintf(&b, "- **%s**\n", name)
		}
		return b.String()

	case "run":
		if len(args) < 2 {
			return "Please specify a cron job name. Usage: `/cron run <name>`"
		}

		jobName := args[1]
		job, ok := jobs[jobName]
		if !ok {
			return fmt.Sprintf("Cron job **%s** not found.", jobName)
		}

		go job.Run()

		return fmt.Sprintf("Cron job **%s** started successfully.", jobName)

	default:
		return "Unknown subcommand. Usage: `/cron list` or `/cron run <name>`"
	}
}
