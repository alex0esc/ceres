package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	filePath string
	mu       sync.RWMutex
	data     map[string]any
}

// New loads the configuration file once at startup.
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

// ReadEntry reads a value using dot notation (e.g., "server.port").
// If the key does not exist yet, defaultValue is inserted into the file and
// returned. Only the missing key is written; existing values, comments and
// formatting are left untouched. Safe to call concurrently from multiple
// goroutines.
//
// Invalid configuration is fatal: a type mismatch, an unreachable key path or
// a failed write terminates the program instead of silently falling back to
// the default, so the user sees the actual config error.
func ReadEntry[T any](c *Config, key string, defaultValue T) T {
	if c == nil {
		log.Fatal("config: ReadEntry called before the config was initialized")
	}
	keys := strings.Split(key, ".")
	for _, part := range keys {
		if part == "" {
			log.Fatalf("config %s: invalid key %q", c.filePath, key)
		}
	}

	c.mu.RLock()
	val, found, err := lookupValue(c.data, keys)
	c.mu.RUnlock()
	if err != nil {
		log.Fatalf("config %s: invalid key %q: %v", c.filePath, key, err)
	}
	if found {
		return mustConvert(c, key, val, defaultValue)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check: another goroutine might have inserted it already
	// while we were waiting for the write lock.
	if val, found, err = lookupValue(c.data, keys); err != nil {
		log.Fatalf("config %s: invalid key %q: %v", c.filePath, key, err)
	}
	if found {
		return mustConvert(c, key, val, defaultValue)
	}

	if c.data == nil {
		c.data = make(map[string]any)
	}
	setNestedValue(c.data, keys, defaultValue)
	if err := c.insertMissing(keys, defaultValue); err != nil {
		log.Fatalf("config %s: failed to persist default for key %q: %v", c.filePath, key, err)
	}
	return defaultValue
}

func mustConvert[T any](c *Config, key string, val any, defaultValue T) T {
	converted, err := convertType[T](val)
	if err != nil {
		log.Fatalf("config %s: value of key %q has the wrong type (%T, expected %T): %v",
			c.filePath, key, val, defaultValue, err)
	}
	return converted
}

func (c *Config) ensureFile() error {
	_, err := os.Stat(c.filePath)
	switch {
	case err == nil:
		return nil
	case !os.IsNotExist(err):
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.filePath, nil, 0o644)
}

func (c *Config) load() error {
	raw, err := os.ReadFile(c.filePath)
	if err != nil {
		return err
	}
	var data map[string]any
	if err := toml.Unmarshal(raw, &data); err != nil {
		return err
	}
	c.data = data
	return nil
}

// lookupValue walks a dotted key path. It reports whether the key exists and
// fails when an intermediate value is not a table (e.g. "agent" is a string
// but "agent.description" was requested).
func lookupValue(m map[string]any, keys []string) (any, bool, error) {
	if len(keys) == 0 || m == nil {
		return nil, false, nil
	}
	val, exists := m[keys[0]]
	if !exists {
		return nil, false, nil
	}
	if len(keys) == 1 {
		return val, true, nil
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("key %q is not a table but %T", keys[0], val)
	}
	return lookupValue(nested, keys[1:])
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

// convertType converts a decoded TOML value into T. go-toml already knows all
// TOML representations (ints, floats, times, tables, arrays of tables), so a
// TOML round-trip handles every supported type. time.Duration is special: TOML
// stores it as a human readable string ("2m0s"), which go-toml does not map
// back to time.Duration on its own.
func convertType[T any](val any) (T, error) {
	if v, ok := val.(T); ok {
		return v, nil
	}
	var zero T

	if _, isDuration := any(zero).(time.Duration); isDuration {
		d, err := toDuration(val)
		if err != nil {
			return zero, err
		}
		return any(d).(T), nil
	}

	data, err := toml.Marshal(struct {
		Value any `toml:"value"`
	}{Value: val})
	if err != nil {
		return zero, err
	}
	var out struct {
		Value T `toml:"value"`
	}
	if err := toml.Unmarshal(data, &out); err != nil {
		return zero, err
	}
	return out.Value, nil
}

// toDuration converts a decoded TOML value into a time.Duration. Durations are
// written as strings ("2m0s"), but plain integers (nanoseconds) are accepted as
// well so hand-edited files keep working.
func toDuration(val any) (time.Duration, error) {
	switch v := val.(type) {
	case time.Duration:
		return v, nil
	case string:
		return time.ParseDuration(v)
	case int64:
		return time.Duration(v), nil
	case int:
		return time.Duration(v), nil
	case float64:
		return time.Duration(v), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to time.Duration", val)
	}
}
