package wakeup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/internal/task"
)

// userWakeupConfig represents the structure of the user's TOML file.
type userWakeupConfig struct {
	Wakeups []userWakeupEntry `toml:"wakeups"`
}

// userWakeupEntry represents a single entry in the TOML file.
type userWakeupEntry struct {
	AgentName   string    `toml:"agent_name"`
	Name        string    `toml:"name"`
	Description string    `toml:"description"`
	FireAt      time.Time `toml:"fire_at"`   // Zero value (time.Time{}) if not set
	CronSpec    string    `toml:"cron_spec"` // Empty string ("") if not set
	TaskType    string    `toml:"task_type"`
	Prompts     []string  `toml:"prompts"`
	Timeout     string    `toml:"timeout"`   // e.g. "5s", "2m"
}

// LoadWakeupsFromFile loads the user wakeups from the TOML config file.
// It returns a map indexed by agent name (map[agentName][]*WakeUp).
// Expired one-shot wakeups are automatically skipped and not loaded.
func LoadWakeupsFromFile() (map[string][]*WakeUp, error) {
	path := constants.WakeupConfigPath

	// Ensure the directory for the config file exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create wakeup config directory: %w", err)
	}

	// Create default config file if it doesn't exist
	if err := ensureWakeupConfigFile(path); err != nil {
		return nil, err
	}

	var cfg userWakeupConfig
	
	// If the file does not exist, simply return an empty map (no error).
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return make(map[string][]*WakeUp), nil
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode user wakeups config %q: %w", path, err)
	}

	result := make(map[string][]*WakeUp)
	now := time.Now()

	for i, entry := range cfg.Wakeups {
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

// ensureWakeupConfigFile creates a default wakeups.toml file if it doesn't exist.
// The example wakeup is an expired one-shot, so it won't be loaded but serves
// as documentation for the user on how to define wakeups.
func ensureWakeupConfigFile(path string) error {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return nil // File exists, nothing to do
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check wakeup config file: %w", err)
	}

	// Create default config with an expired one-shot example
	cfg := userWakeupConfig{
		Wakeups: []userWakeupEntry{
			{
				AgentName:   "Ceres",
				Name:        "example_wakeup",
				Description: "This is an example wakeup. Edit this file to add your own wakeups. This example is already expired and will be ignored.",
				FireAt:      time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), // Expired one-shot - will be ignored
				CronSpec:    "",
				TaskType:    "ask",
				Prompts:     []string{"This is an example prompt. Replace it with your own task."},
				Timeout:     "30s",
			},
		},
	}

	// Create the file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create default wakeup config file: %w", err)
	}
	defer file.Close()

	// Write a comprehensive header comment explaining the file
	header := `# Wakeup Configuration File
#
# This file defines scheduled tasks (wakeups) for your agents.
# Each wakeup can be either:
#   - One-shot: Set fire_at to a specific time (use ISO 8601 format, e.g. 2026-09-25T10:00:00Z)
#   - Recurring: Set cron_spec = "..." to a cron expression (e.g. "0 18 * * *" for daily at 6 PM)
#
# IMPORTANT: Expired one-shot wakeups are automatically ignored and not executed.
# Edit this file to add your own wakeups, then restart the application.
#
# Task Types:
#   - "ask": Send a message to the agent, current chat history is preserved
#   - "clear": Clear the agent's chat history (no prompt sent)
#   - "compress": Compress the agent's chat history to save context space
#   - "clear_ask": Clear the chat history first, then send the prompt
#
# Timeout: Duration for the task execution (e.g. "30s", "2m", "1h")
# Prompts: List of text prompts to send to the agent (for "ask" and "clear_ask")
#
`
	if _, err := file.WriteString(header); err != nil {
		return fmt.Errorf("failed to write config header: %w", err)
	}

	// Write the config
	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("failed to write default wakeup config file: %w", err)
	}

	return nil
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
	case "ask", "":
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
