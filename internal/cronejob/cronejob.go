package cronjob

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/robfig/cron/v3"
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

// DefaultLocation is used when no location is set in the config file.
const DefaultLocation = "Local"

// LoadCronJobsFromFile reads chronejobs.toml from path and returns the
// parsed, validated list of job configs together with the shared
// *time.Location that all schedules should be interpreted in.
func LoadCronJobsFromFile(path string) ([]CronJobConfig, *time.Location, error) {
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
		locName = DefaultLocation
	}
	loc, err := time.LoadLocation(locName)
	if err != nil {
		return nil, nil, fmt.Errorf("cron jobs config %q: invalid location %q: %w", path, locName, err)
	}

	for i, job := range file.Jobs {
		if job.Name == "" {
			return nil, nil, fmt.Errorf("invalid cron job at index %d: missing name", i)
		}
		if job.AgentName == "" {
			return nil, nil, fmt.Errorf("cron job %q: missing agent_name", job.Name)
		}
		if len(job.Times) == 0 {
			return nil, nil, fmt.Errorf("cron job %q: missing at least one entry in times", job.Name)
		}
		if len(job.Prompts) == 0 {
			return nil, nil, fmt.Errorf("cron job %q: missing at least one entry in prompts", job.Name)
		}
		// Validate timeout syntax if provided (e.g. "5m", "10s", "1h")
		if job.Timeout == "" {
			return nil, nil, fmt.Errorf("cron job %q: missing timeout duration", job.Name)
		}
		if _, err := time.ParseDuration(job.Timeout); err != nil {
			return nil, nil, fmt.Errorf("cron job %q: invalid timeout duration %q: %w", job.Name, job.Timeout, err)
		}
	}
	return file.Jobs, loc, nil
}

// RegisterCronJobs adds every job's schedules to the given cron.Cron instance.
// handler is invoked with the job's config each time one of its schedules
// fires, so a job with multiple entries in Times can share one handler call
// signature regardless of which schedule triggered it.
func RegisterCronJobs(c *cron.Cron, jobs []CronJobConfig, checker func(CronJobConfig) error, handler func(CronJobConfig)) error {
	for _, job := range jobs {
		err := checker(job)
		if err != nil {
			return fmt.Errorf("invalid cronejob %s detected: %v", job.Name, err)
		}
		for _, spec := range job.Times {
			if _, err := c.AddFunc(spec, func() {
				handler(job)
			}); err != nil {
				return fmt.Errorf("cron job %q: invalid schedule %q: %w", job.Name, spec, err)
			}
		}
	}
	return nil
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
