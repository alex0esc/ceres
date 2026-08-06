package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

type Config struct {
	filePath string
	mu       sync.RWMutex
	data     map[string]any
}

// NewConfig loads the configuration file once at startup.
func New(path string) (*Config, error) {
	cfg := &Config{filePath: path}
	if err := cfg.ensureFile(); err != nil {
		return nil, err
	}
	if err := cfg.load(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ReadConfigEntry reads a value using dot notation (e.g., "server.port").
// If the key does not exist yet, defaultValue is written to the file.
// Safe to call concurrently from multiple goroutines.
func ReadEntry[T any](c *Config, key string, defaultValue T) T {
	keys := strings.Split(key, ".")

	c.mu.RLock()
	val, found := getNestedValue(c.data, keys)
	c.mu.RUnlock()

	if !found {
		c.mu.Lock()
		defer c.mu.Unlock()
		// Double-check: another goroutine might have written it already
		// while we were waiting for the write lock.
		if val, found = getNestedValue(c.data, keys); !found {
			if c.data == nil {
				c.data = make(map[string]any)
			}
			setNestedValue(c.data, keys, defaultValue)
			_ = c.save()
			return defaultValue
		}
	}

	if casted, ok := convertType[T](val); ok {
		return casted
	}
	return defaultValue
}

// --- Internal Helper Functions ---

func (c *Config) save() error {
	f, err := os.Create(c.filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c.data)
}

func (c *Config) ensureFile() error {
	if _, err := os.Stat(c.filePath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(c.filePath), 0755); err != nil {
			return err
		}
		return os.WriteFile(c.filePath, []byte("# Dynamic TOML Config\n"), 0644)
	}
	return nil
}

func (c *Config) load() error {
	var data map[string]any
	if _, err := toml.DecodeFile(c.filePath, &data); err != nil {
		return err
	}
	c.data = data
	return nil
}

func getNestedValue(m map[string]any, keys []string) (any, bool) {
	if len(keys) == 0 || m == nil {
		return nil, false
	}
	val, exists := m[keys[0]]
	if !exists {
		return nil, false
	}
	if len(keys) == 1 {
		return val, true
	}
	if nestedMap, ok := val.(map[string]any); ok {
		return getNestedValue(nestedMap, keys[1:])
	}
	return nil, false
}

func setNestedValue(m map[string]any, keys []string, value any) {
	if len(keys) == 0 {
		return
	}
	if len(keys) == 1 {
		m[keys[0]] = value
		return
	}
	nextMap, exists := m[keys[0]]
	if !exists {
		nextMap = make(map[string]any)
		m[keys[0]] = nextMap
	}
	if nestedMap, ok := nextMap.(map[string]any); ok {
		setNestedValue(nestedMap, keys[1:], value)
	}
}


func convertType[T any](val any) (T, bool) {
	var zero T
	if v, ok := val.(T); ok {
		return v, true
	}

	switch any(zero).(type) {
	case int:
		if v, ok := val.(int64); ok {
			return any(int(v)).(T), true
		}
	case float64:
		if v, ok := val.(int64); ok {
			return any(float64(v)).(T), true
		}
	case []string:
		if rawSlice, ok := val.([]any); ok {
			strSlice := make([]string, 0, len(rawSlice))
			for _, item := range rawSlice {
				if str, ok := item.(string); ok {
					strSlice = append(strSlice, str)
				} else {
					return zero, false 
				}
			}
			return any(strSlice).(T), true
		}
	}
	return zero, false
}
