package cronjob

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/robfig/cron/v3"
)

//wrapper for crone jobs
type CronJob struct {
	name    string
	agent   *agent.Agent
	times   []string
	task    handles.Task
	timeout time.Duration
}

func NewCronJob(name string, ag *agent.Agent, times []string, task handles.Task, timeout time.Duration) *CronJob {
	return &CronJob{
		name:    name,
		agent:   ag,
		times:   times,
		task:    task,
		timeout: timeout,
	}
}

// executes the cron job
func (cj *CronJob) Run() {
	res := <-cj.agent.SubmitTask(&cj.task)
	if res.Err != nil {
		slog.Error(fmt.Sprintf("error while running crone job %s: %v", cj.name, res.Err))
	}
}

// registers cron jobs for the cron.Cron
func (cj *CronJob) Register(c *cron.Cron) error {
	for _, spec := range cj.times {
		if _, err := c.AddFunc(spec, func() {
			cj.Run()
		}); err != nil {
			return fmt.Errorf("cron job %q: invalid schedule %q: %w", cj.name, spec, err)
		}
	}
	return nil
}
