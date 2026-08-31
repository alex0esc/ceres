package cronjob

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alex0esc/ceres/internal/agent"
	"github.com/alex0esc/ceres/pkg/handles"
)

// CronJobConfig is the on-disk representation of a single cron job entry,
// loaded from chronejobs.toml.
type CronJobConfig struct {
	Name      string   `toml:"name"`
	AgentName string   `toml:"agent_name"`
	Times     []string `toml:"times"`
	Prompts   []string `toml:"prompts"`
	Timeout   string   `toml:"timeout,omitempty"`
}

// CronJobsFile is the top-level structure of chronejobs.toml: a global
// location for all jobs, plus a list of jobs under the "jobs" array-of-tables
// key ([[jobs]]).
type CronJobsFile struct {
	Location string          `toml:"location,omitempty"`
	Jobs     []CronJobConfig `toml:"jobs"`
}


// LoadCronJobsFromFile reads chronejobs.toml, validates all entries including agent existence,
// and returns a map of *CronJob indexed by job name together with the shared *time.Location.
func LoadCronJobsFromFile(path string, agents map[string]*agent.Agent) (map[string]*CronJob, *time.Location, error) {
	err := EnsureDefaultCronJobsFile(path)
	if err != nil {
		return nil, nil, err
	}
	var file CronJobsFile
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return nil, nil, fmt.Errorf("failed to decode cron jobs config %q: %w", path, err)
	}

	locName := file.Location
	if locName == "" {
		return nil, nil, fmt.Errorf("failed to load cronejobs config, missing attribute location")
	}
	loc, err := time.LoadLocation(locName)
	if err != nil {
		return nil, nil, fmt.Errorf("cron jobs config %q: invalid location %q: %w", path, locName, err)
	}

	jobsMap := make(map[string]*CronJob)

	for i, job := range file.Jobs {
		if job.Name == "" {
			return nil, nil, fmt.Errorf("invalid cron job at index %d: missing name", i)
		}
		if job.AgentName == "" {
			return nil, nil, fmt.Errorf("cron job %q: missing agent_name", job.Name)
		}
		// Prüfung, ob der angegebene Agent existiert
		if _, ok := agents[job.AgentName]; !ok {
			return nil, nil, fmt.Errorf("cron job %q: agent %q does not exist", job.Name, job.AgentName)
		}
		if len(job.Times) == 0 {
			return nil, nil, fmt.Errorf("cron job %q: missing at least one entry in times", job.Name)
		}
		if len(job.Prompts) == 0 {
			return nil, nil, fmt.Errorf("cron job %q: missing at least one entry in prompts", job.Name)
		}
		if job.Timeout == "" {
			return nil, nil, fmt.Errorf("cron job %q: missing timeout duration", job.Name)
		}

		timeout, err := time.ParseDuration(job.Timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("cron job %q: invalid timeout duration %q: %w", job.Name, job.Timeout, err)
		}

		jobsMap[job.Name] = NewCronJob(
			job.Name,
			agents[job.AgentName],
			job.Times,
			handles.TaskClearAskMultiple(job.Prompts, timeout),
			timeout,
		)
	}

	return jobsMap, loc, nil
}


// EnsureDefaultCronJobsFile creates a chronejobs.toml with a default
// location and one example job if no such file exists yet at path. Mirrors
// EnsureOneAgentFile's behavior for agent configs.
func EnsureDefaultCronJobsFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // file already exists, nothing to do
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat cron jobs config %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create cron jobs directory: %w", err)
	}
	cfg := CronJobsFile{
		Location: "Europe/Berlin",
		Jobs: []CronJobConfig{
			{
				Name:      "portfolio-check",
				AgentName: "Ceres",
				Times:     []string{"0 9 * * 1-5"},
				Prompts:   []string{"Check all stocks in the portfolio and decide whether to buy, sell, or hold based on the latest news."},
				Timeout:   "20m",
			},
		},
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create default cron jobs file: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("failed to write default cron jobs file: %w", err)
	}
	return nil
}
