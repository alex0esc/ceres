package wakeup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/pkg/config"
)

// userWakeupEntry represents a single entry in the TOML file.
type userWakeupEntry struct {
	AgentName   string    `toml:"agent_name"`
	Name        string    `toml:"name"`
	Description string    `toml:"description"`
	FireAt      time.Time `toml:"fire_at"`   // Zero value (time.Time{}) if not set
	CronSpec    string    `toml:"cron_spec"` // Empty string ("") if not set
	TaskType    string    `toml:"task_type"`
	Prompts     []string  `toml:"prompts"`
	Timeout     string    `toml:"timeout"` // e.g. "5s", "2m"
}

// LoadWakeupsFromFile loads the user wakeups from the TOML config file.
// It returns a map indexed by agent name (map[agentName][]*WakeUp).
// Expired one-shot wakeups are automatically skipped and not loaded.
func LoadWakeupsFromFile() (map[string][]*WakeUp, error) {
	cfg, err := config.New(constants.WakeupConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load wakeup config %q: %w", constants.WakeupConfigPath, err)
	}

	// The example entry is an expired one-shot, so it documents the format in
	// the config file but is never executed.
	defaults := []userWakeupEntry{
		{
			AgentName:   "Ceres",
			Name:        "example_wakeup",
			Description: "This is an example wakeup. Edit this file to add your own wakeups. This example is already expired and will be ignored.",
			FireAt:      time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			CronSpec:    "",
			TaskType:    "clear_ask",
			Prompts:     []string{"This is an example prompt. Replace it with your own task."},
			Timeout:     "30s",
		},
	}

	result := make(map[string][]*WakeUp)
	now := time.Now()

	for i, entry := range config.ReadEntry(cfg, "wakeups", defaults) {
		wu, err := parseUserWakeupEntry(entry, now)
		if err != nil {
			return nil, fmt.Errorf("invalid wakeup entry %d (%s): %w", i, entry.Name, err)
		}
		// wu is nil if it is an expired one-shot wakeup.
		if wu == nil {
			continue
		}
		result[entry.AgentName] = append(result[entry.AgentName], wu)
	}
	return result, nil
}

// parseUserWakeupEntry validates a config entry and converts it into a WakeUp object.
func parseUserWakeupEntry(entry userWakeupEntry, now time.Time) (*WakeUp, error) {
	if strings.TrimSpace(entry.AgentName) == "" {
		return nil, fmt.Errorf("missing agent_name")
	}
	if strings.TrimSpace(entry.Name) == "" {
		return nil, fmt.Errorf("missing name")
	}

	hasFireAt := !entry.FireAt.IsZero()
	hasCronSpec := entry.CronSpec != ""

	// Exactly one of these must be set.
	if hasFireAt == hasCronSpec {
		return nil, fmt.Errorf("exactly one of fire_at or cron_spec must be set")
	}

	// IMPORTANT: Expired one-shot wakeups are not loaded at all.
	if hasFireAt && entry.FireAt.Before(now) {
		return nil, nil
	}

	timeout, err := time.ParseDuration(entry.Timeout)
	if err != nil {
		return nil, fmt.Errorf("invalid timeout %q: %w", entry.Timeout, err)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}

	var tType task.TaskType
	switch strings.ToLower(strings.TrimSpace(entry.TaskType)) {
	case "ask":
		tType = task.TaskTypeAsk
	case "clear":
		tType = task.TaskTypeClear
	case "clear_ask":
		tType = task.TaskTypeClearAsk
	case "compress":
		tType = task.TaskTypeCompress
	default:
		return nil, fmt.Errorf("unknown task_type %q", entry.TaskType)
	}

	if len(entry.Prompts) == 0 && (tType == task.TaskTypeAsk || tType == task.TaskTypeClearAsk) {
		return nil, fmt.Errorf("prompts list cannot be empty for task_type %q", entry.TaskType)
	}

	var prompts []task.Prompt
	for _, p := range entry.Prompts {
		prompts = append(prompts, task.Prompt{Text: p})
	}

	tsk := task.Task{
		ParentCtx: context.Background(),
		Timeout:   timeout,
		Prompts:   prompts,
		Tasktype:  tType,
	}

	// We use the NewWakeUp constructor from the wakeup class.
	// entry.FireAt and entry.CronSpec are passed directly, as their zero values
	// (time.Time{} and "") perfectly match the expectations of NewWakeUp.
	wu := NewWakeUp(entry.Name, entry.Description, entry.FireAt, entry.CronSpec, tsk)
	return wu, nil
}
