package history

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/alex0esc/ceres/internal/constants"
)

// Each agent's whole chat history is stored as a single row (one row per agent),
// serialized to JSON in the history column. The table is created on open.
const storageSchema = `
CREATE TABLE IF NOT EXISTS histories (
	agent       TEXT PRIMARY KEY,
	history     TEXT NOT NULL,   -- JSON of History (entries + total_tokens)
	updated_at  INTEGER NOT NULL DEFAULT (unixepoch())
);`

const (
	storageUpsert = `
INSERT INTO histories (agent, history)
VALUES (?, ?)
ON CONFLICT(agent) DO UPDATE SET
	history    = excluded.history,
	updated_at = unixepoch()`

	storageSelect = `SELECT history FROM histories WHERE agent = ?`
)

// ErrNotOpen is returned when the history store is used before StorageOpen.
var ErrNotOpen = errors.New("history: store not opened")

// storageDB is the global history database. StorageOpen must be called once at
// startup, before StorageSave/StorageLoad are used.
var storageDB *sql.DB

// StorageOpen opens (and creates if needed) the global history database.
func StorageOpen() error {
	if storageDB != nil {
		return nil
	}

	path := constants.HistoryDatabasePath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("history: failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return fmt.Errorf("history: failed to open database: %w", err)
	}

	if _, err := db.Exec(storageSchema); err != nil {
		db.Close()
		return fmt.Errorf("history: failed to create histories table: %w", err)
	}

	storageDB = db
	return nil
}

// StorageClose closes the global history database.
func StorageClose() {
	if storageDB != nil {
		storageDB.Close()
		storageDB = nil
	}
}

// StorageSave persists the given history for the agent, overwriting any
// previously stored history for that agent (one row per agent).
func StorageSave(agentName string, h History) error {
	if storageDB == nil {
		return ErrNotOpen
	}

	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("history: failed to encode history for %q: %w", agentName, err)
	}

	if _, err := storageDB.Exec(storageUpsert, agentName, string(data)); err != nil {
		return fmt.Errorf("history: failed to save history for %q: %w", agentName, err)
	}
	return nil
}

// StorageLoad returns the persisted history for the agent. If no history is
// stored yet, an empty History is returned.
func StorageLoad(agentName string) (History, error) {
	if storageDB == nil {
		return History{}, ErrNotOpen
	}

	var raw string
	err := storageDB.QueryRow(storageSelect, agentName).Scan(&raw)
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
