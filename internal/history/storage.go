package history

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/alex0esc/ceres/internal/agentdb"
)

// The chat history is stored as one row per entry in history_entries. Nested
// values (reasoning, tool calls/results, images) keep their JSON shape in their
// own column; the history-wide token count lives in history_meta.
const (
	entriesSchema = `
CREATE TABLE IF NOT EXISTS history_entries (
	seq         INTEGER PRIMARY KEY,
	type        INTEGER NOT NULL,
	text        TEXT,
	reasoning   TEXT,   -- JSON of Reasoning, nullable
	tool_call   TEXT,   -- JSON of ToolCall, nullable
	tool_result TEXT,   -- JSON of ToolResult, nullable
	images      TEXT,   -- JSON of []Image, nullable
	timestamp   INTEGER NOT NULL DEFAULT 0
);`

	metaSchema = `
CREATE TABLE IF NOT EXISTS history_meta (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	total_tokens INTEGER NOT NULL DEFAULT 0
);`

	entriesInsert = `
INSERT INTO history_entries
	(seq, type, text, reasoning, tool_call, tool_result, images, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	entriesSelect = `
SELECT seq, type, text, reasoning, tool_call, tool_result, images, timestamp
FROM history_entries ORDER BY seq`

	metaUpsert = `
INSERT INTO history_meta (id, total_tokens) VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET total_tokens = excluded.total_tokens`

	metaSelect = `SELECT total_tokens FROM history_meta WHERE id = 1`
)

// schemaMu guards the per-agent schema initialisation and legacy migration.
// The connection itself is owned by agentdb; each package creates its own
// tables on first use.
var (
	schemaMu sync.Mutex
	schemas  = make(map[string]bool)
)

// historyDB returns the shared persistent handle for the agent and makes sure
// the history tables exist (migrating an old JSON blob if present).
func historyDB(agentName string) (*sql.DB, error) {
	dbs, err := agentdb.Open(agentName)
	if err != nil {
		return nil, err
	}

	schemaMu.Lock()
	defer schemaMu.Unlock()
	if schemas[agentName] {
		return dbs.Persistent, nil
	}
	if err := initialiseSchema(dbs.Persistent); err != nil {
		return nil, fmt.Errorf("history: failed to initialise storage for %q: %w", agentName, err)
	}
	schemas[agentName] = true
	return dbs.Persistent, nil
}

// initialiseSchema creates the new tables and, if an old histories table with
// a JSON blob exists, moves its contents over and drops it. Everything happens
// in one transaction so a crash cannot leave a half-migrated database.
func initialiseSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(entriesSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(metaSchema); err != nil {
		return err
	}

	var name string
	err = tx.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'histories'`).Scan(&name)
	switch {
	case err == nil:
		legacy, err := loadLegacy(tx)
		if err != nil {
			return err
		}
		if legacy != nil {
			if err := saveHistory(tx, *legacy); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DROP TABLE histories`); err != nil {
			return err
		}
	case errors.Is(err, sql.ErrNoRows):
		// fresh database, nothing to migrate
	default:
		return err
	}

	return tx.Commit()
}

// loadLegacy decodes the single JSON row used by the old storage format.
func loadLegacy(tx *sql.Tx) (*History, error) {
	var raw sql.NullString
	err := tx.QueryRow(`SELECT history FROM histories WHERE id = 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var h History
	if err := json.Unmarshal([]byte(raw.String), &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// optionalJSON returns NULL unless present is true, so empty nested fields do
// not waste space and decode back to their zero value.
func optionalJSON(v any, present bool) (any, error) {
	if !present {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// saveHistory replaces all stored entries with h. The caller owns the
// transaction so the same code can be used by StorageSave and the migration.
func saveHistory(tx *sql.Tx, h History) error {
	if _, err := tx.Exec(`DELETE FROM history_entries`); err != nil {
		return err
	}

	stmt, err := tx.Prepare(entriesInsert)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, entry := range h.Entries {
		reasoning, err := optionalJSON(entry.Reasoning, entry.Reasoning != nil)
		if err != nil {
			return err
		}
		call, err := optionalJSON(entry.ToolCall, entry.ToolCall != nil)
		if err != nil {
			return err
		}
		result, err := optionalJSON(entry.ToolResult, entry.ToolResult != nil)
		if err != nil {
			return err
		}
		images, err := optionalJSON(entry.Images, len(entry.Images) > 0)
		if err != nil {
			return err
		}

		if _, err := stmt.Exec(
			i, entry.Type, nullString(entry.Text),
			reasoning, call, result, images, entry.Timestamp,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(metaUpsert, h.TotalTokens); err != nil {
		return err
	}
	return nil
}

// loadEntries materialises every row before returning, so callers can run
// another query on the shared single-connection handle without deadlocking.
func loadEntries(db *sql.DB) ([]Entry, error) {
	rows, err := db.Query(entriesSelect)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		var (
			seq                                   int64
			typeValue                             int64
			text, reasoning, call, result, images sql.NullString
			timestamp                             int64
		)
		if err := rows.Scan(&seq, &typeValue, &text, &reasoning, &call, &result, &images, &timestamp); err != nil {
			return nil, err
		}

		entry := Entry{Type: EntryType(typeValue), Text: text.String, Timestamp: timestamp}
		if err := decodeField(reasoning, &entry.Reasoning); err != nil {
			return nil, err
		}
		if err := decodeField(call, &entry.ToolCall); err != nil {
			return nil, err
		}
		if err := decodeField(result, &entry.ToolResult); err != nil {
			return nil, err
		}
		if err := decodeField(images, &entry.Images); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func decodeField(raw sql.NullString, dst any) error {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw.String), dst)
}

// StorageSave persists the given history for the agent, overwriting any
// previously stored history for that agent.
func StorageSave(agentName string, h History) error {
	db, err := historyDB(agentName)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("history: failed to begin save for %q: %w", agentName, err)
	}
	defer tx.Rollback()

	if err := saveHistory(tx, h); err != nil {
		return fmt.Errorf("history: failed to save history for %q: %w", agentName, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("history: failed to commit history for %q: %w", agentName, err)
	}
	return nil
}

// StorageLoad returns the persisted history for the agent. If no history is
// stored yet, an empty History is returned.
func StorageLoad(agentName string) (History, error) {
	db, err := historyDB(agentName)
	if err != nil {
		return History{}, err
	}

	entries, err := loadEntries(db)
	if err != nil {
		return History{}, fmt.Errorf("history: failed to load history for %q: %w", agentName, err)
	}

	var tokens int64
	err = db.QueryRow(metaSelect).Scan(&tokens)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return History{}, fmt.Errorf("history: failed to load token count for %q: %w", agentName, err)
	}

	return History{Entries: entries, TotalTokens: tokens}, nil
}
