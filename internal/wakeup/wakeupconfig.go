package wakeup

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/pkg/handles"
)

// WakeupEntry is the on-disk representation of a single scheduled wakeup.
type WakeupEntry struct {
	Name string `toml:"name"`

	FireAt   *time.Time `toml:"fire_at,omitempty"`   // one-shot
	CronSpec string     `toml:"cron_spec,omitempty"` // recurring

	Description string   `toml:"description"`
	Task        []string `toml:"task"`
	Timeout     string   `toml:"timeout"` // e.g. "20m", parsed via time.ParseDuration

	Protected bool      `toml:"protected"`
	CreatedAt time.Time `toml:"created_at"`
}

// WakeupFile is the top-level structure of a single agent's wakeup file.
type WakeupFile struct {
	Wakeups []WakeupEntry `toml:"wakeups"`
}

var fileMu sync.Mutex

// LoadWakeupsForAgent reads "<agent-name>.toml" from dir and returns the
// agent's wakeups keyed by wakeup name. Each entry's own timeout is used
// for its task, since different wakeups can carry very different tasks.
func LoadWakeupsForAgent(ag handles.AgentHandle) (map[string]*WakeUp, error) {
	if err := os.MkdirAll(constants.WakeupFolderPath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create wakeups directory %q: %w", constants.WakeupFolderPath, err)
	}

	path := filepath.Join(constants.WakeupFolderPath, ag.Name()+".toml")

	fileMu.Lock()
	wf, err := readWakeupFile(path)
	fileMu.Unlock()
	if err != nil {
		return nil, err
	}

	wakeups := make(map[string]*WakeUp, len(wf.Wakeups))
	for i, we := range wf.Wakeups {
		if we.Name == "" {
			return nil, fmt.Errorf("invalid wakeup at index %d in %q: missing name", i, path)
		}
		if len(we.Task) == 0 {
			return nil, fmt.Errorf("wakeup %q: missing at least one entry in task", we.Name)
		}
		if we.Timeout == "" {
			return nil, fmt.Errorf("wakeup %q: missing timeout duration", we.Name)
		}

		timeout, err := time.ParseDuration(we.Timeout)
		if err != nil {
			return nil, fmt.Errorf("wakeup %q: invalid timeout duration %q: %w", we.Name, we.Timeout, err)
		}

		hasFireAt := we.FireAt != nil
		hasCron := we.CronSpec != ""
		if hasFireAt == hasCron {
			return nil, fmt.Errorf("wakeup %q: exactly one of fire_at or cron_spec must be set", we.Name)
		}

		wakeups[we.Name] = NewWakeUp(
			we.Name,
			we.Description,
			ag,
			we.FireAt,
			we.CronSpec,
			we.Task,
			timeout,
			we.Protected,
		)
	}

	return wakeups, nil
}

// setWakeup persists entry (upsert by name) to ag's wakeup file. protected
// is always forced to false here - only the system may create protected
// entries, never an agent itself.
func setWakeup(ag handles.AgentHandle, entry WakeupEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("wakeup: missing name")
	}
	if len(entry.Task) == 0 {
		return fmt.Errorf("wakeup %q: missing at least one entry in task", entry.Name)
	}
	hasFireAt := entry.FireAt != nil
	hasCron := entry.CronSpec != ""
	if hasFireAt == hasCron {
		return fmt.Errorf("wakeup %q: exactly one of fire_at or cron_spec must be set", entry.Name)
	}
	if entry.Timeout == "" {
		return fmt.Errorf("wakeup %q: missing timeout duration", entry.Name)
	}
	if _, err := time.ParseDuration(entry.Timeout); err != nil {
		return fmt.Errorf("wakeup %q: invalid timeout duration %q: %w", entry.Name, entry.Timeout, err)
	}

	entry.Protected = false
	entry.CreatedAt = time.Now()

	if err := os.MkdirAll(constants.WakeupFolderPath, 0o755); err != nil {
		return fmt.Errorf("failed to create wakeups directory %q: %w", constants.WakeupFolderPath, err)
	}

	fileMu.Lock()
	defer fileMu.Unlock()

	path := filepath.Join(constants.WakeupFolderPath, ag.Name()+".toml")

	wf, err := readWakeupFile(path)
	if err != nil {
		return err
	}

	replaced := false
	for i, e := range wf.Wakeups {
		if e.Name == entry.Name {
			wf.Wakeups[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		wf.Wakeups = append(wf.Wakeups, entry)
	}

	return writeWakeupFile(path, wf)
}

// removeWakeup deletes a single wakeup entry from ag's file.
func removeWakeup(ag handles.AgentHandle, name string) error {
	fileMu.Lock()
	defer fileMu.Unlock()

	path := filepath.Join(constants.WakeupFolderPath, ag.Name()+".toml")

	wf, err := readWakeupFile(path)
	if err != nil {
		return err
	}

	filtered := wf.Wakeups[:0]
	for _, e := range wf.Wakeups {
		if e.Name != name {
			filtered = append(filtered, e)
		}
	}
	wf.Wakeups = filtered

	return writeWakeupFile(path, wf)
}

// readWakeupFile reads an agent's wakeup file, returning an empty
// WakeupFile if it doesn't exist yet. Callers must hold fileMu.
func readWakeupFile(path string) (WakeupFile, error) {
	var wf WakeupFile
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return wf, nil
	}
	if _, err := toml.DecodeFile(path, &wf); err != nil {
		return wf, fmt.Errorf("failed to decode wakeup file %q: %w", path, err)
	}
	return wf, nil
}

// writeWakeupFile persists wf to path atomically (temp file + rename in
// the same directory). Callers must hold fileMu.
func writeWakeupFile(path string, wf WakeupFile) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to create temp wakeup file: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(wf); err != nil {
		f.Close()
		return fmt.Errorf("failed to encode wakeup file: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
