package history

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/alex0esc/ceres/internal/constants"
)

// Each agent's whole chat history is stored as a single row in the agent's
// own persistent_data.db (database/<agent name>/persistent_data.db),
// serialized to JSON in the history column. The table is created on open.
const storageSchema = `
CREATE TABLE IF NOT EXISTS histories (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	history     TEXT NOT NULL,   -- JSON of History (entries + total_tokens)
	updated_at  INTEGER NOT NULL DEFAULT (unixepoch())
);`

const (
	storageUpsert = `
INSERT INTO histories (id, history)
VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET
	history    = excluded.history,
	updated_at = unixepoch()`

	storageSelect = `SELECT history FROM histories WHERE id = 1`
)

// storageDbs holds one open handle per agent database, opened lazily on the
// first access and closed together by StorageCloseAll.
var (
	storageMu  sync.Mutex
	storageDbs = make(map[string]*sql.DB)
)

// storageDB returns the agent's history database, opening (and creating) it
// on first use.
func storageDB(agentName string) (*sql.DB, error) {
	storageMu.Lock()
	defer storageMu.Unlock()

	if db, ok := storageDbs[agentName]; ok {
		return db, nil
	}

	path := constants.AgentPersistentDBPath(agentName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("history: failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("history: failed to open database: %w", err)
	}

	if _, err := db.Exec(storageSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("history: failed to create histories table: %w", err)
	}

	storageDbs[agentName] = db
	return db, nil
}

// StorageCloseAll closes all open history databases.
func StorageCloseAll() {
	storageMu.Lock()
	defer storageMu.Unlock()

	for name, db := range storageDbs {
		db.Close()
		delete(storageDbs, name)
	}
}

// StorageSave persists the given history for the agent, overwriting any
// previously stored history for that agent.
func StorageSave(agentName string, h History) error {
	db, err := storageDB(agentName)
	if err != nil {
		return err
	}

	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("history: failed to encode history for %q: %w", agentName, err)
	}

	if _, err := db.Exec(storageUpsert, string(data)); err != nil {
		return fmt.Errorf("history: failed to save history for %q: %w", agentName, err)
	}
	return nil
}

// StorageLoad returns the persisted history for the agent. If no history is
// stored yet, an empty History is returned.
func StorageLoad(agentName string) (History, error) {
	db, err := storageDB(agentName)
	if err != nil {
		return History{}, err
	}

	var raw string
	err = db.QueryRow(storageSelect).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return History{}, nil
	}
	if err != nil {
		return History{}, fmt.Errorf("history: failed to load history for %q: %w", agentName, err)
	}

	var h History
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		return History{}, fmt.Errorf("history: failed to decode history for %q: %w", agentName, err)
	}
	return h, nil
}
