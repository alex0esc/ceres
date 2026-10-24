package wakeup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/internal/task"
)

const opTimeout = 5 * time.Second

// The wakeup config: prompt wakeups created by the agents themselves, stored
// in the agent's own persistent_data.db (database/<agent name>/
// persistent_data.db), each one owned by the agent of that file. Wakeups from
// the user's config file do not live here.
// Exactly one of fire_at / cron_spec must be set, the prompt must not be
// empty and the timeout of the prompt's task must be positive. All three
// rules are enforced by the database.
const schema = `
CREATE TABLE IF NOT EXISTS wakeups (
	name           TEXT    PRIMARY KEY,

	fire_at        INTEGER,            -- unix seconds, one-shot
	cron_spec      TEXT,               -- recurring

	prompt         TEXT    NOT NULL,
	timeout        INTEGER NOT NULL,   -- seconds, timeout of the prompt's task
	description    TEXT,
	clear_history  INTEGER NOT NULL DEFAULT 0,  -- 0 = keep history (ask), 1 = clear history (clear_ask)

	created_at     INTEGER NOT NULL DEFAULT (unixepoch()),

	CHECK ((fire_at IS NULL) <> (cron_spec IS NULL)),
	CHECK (length(trim(prompt)) > 0),
	CHECK (timeout > 0)
);`

const (
	// The wakeups of the agent owning the database.
	selectSQL = `
SELECT name, fire_at, cron_spec, prompt, timeout, description, clear_history
FROM wakeups ORDER BY name`

	// Insert only: an already existing name is left untouched and
	// reported as RowsAffected == 0.
	insertSQL = `
INSERT INTO wakeups (name, fire_at, cron_spec, prompt, timeout, description, clear_history)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO NOTHING`

	deleteSQL = `DELETE FROM wakeups WHERE name = ?`
)

var (
	ErrNotFound = errors.New("wakeup: not found")
	ErrExists   = errors.New("wakeup: already exists")
)

// dbs holds one open handle per agent database (the agent's
// persistent_data.db), opened lazily on first use and closed by DbCloseAll.
var (
	dbMu  sync.Mutex
	dbs   = make(map[string]*sql.DB)
)

// ---------------------------------------------------------------------------
// Public
// ---------------------------------------------------------------------------

// dbHandle returns the agent's wakeup database, opening (and creating) it on
// first use.
func dbHandle(agentName string) (*sql.DB, error) {
	dbMu.Lock()
	defer dbMu.Unlock()

	if d, ok := dbs[agentName]; ok {
		return d, nil
	}

	path := constants.AgentPersistentDBPath(agentName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create wakeup directory: %w", err)
	}

	d, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("failed to open wakeup database: %w", err)
	}
	// One connection: writes are serialized anyway. Never keep rows open
	// while running another query, that would block forever.
	d.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if _, err := d.ExecContext(ctx, schema); err != nil {
		d.Close()
		return nil, fmt.Errorf("failed to initialise wakeup database: %w", err)
	}

	dbs[agentName] = d
	return d, nil
}

// DbCloseAll closes all open wakeup databases.
func DbCloseAll() {
	dbMu.Lock()
	defer dbMu.Unlock()

	for name, d := range dbs {
		d.Close()
		delete(dbs, name)
	}
}

// DbLoadWakeups reads the wakeups of one agent from its wakeup database and
// returns them as ready-made WakeUps keyed by name. The task of each wakeup
// uses the timeout stored with it. The wakeups are not started; the Manager
// does that and gives them the agent's submit function.
func DbLoadWakeups(agentName string) (map[string]*WakeUp, error) {
	d, err := dbHandle(agentName)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	rows, err := loadRows(ctx, d)
	if err != nil {
		return nil, err
	}

	wakeups := make(map[string]*WakeUp, len(rows))
	for _, r := range rows {
		var fireAt time.Time
		if r.fireAt.Valid {
			t := time.Unix(r.fireAt.Int64, 0)
			fireAt = t
		}

		wakeups[r.name] = newPromptWakeUp(
			r.name,
			r.description.String,
			fireAt,
			r.cronSpec.String,
			r.prompt,
			time.Duration(r.timeoutSecs)*time.Second,
			r.clearHistory,
		)
	}

	return wakeups, nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

// DbAddWakeup stores a new prompt wakeup for the given agent. It never
// replaces anything: if the agent already has a wakeup with this name it
// yields ErrExists, so remove the old one first.
//
// Set exactly one of fireAt/cronSpec (leave the other one as an empty
// string / nil). The prompt must not be empty. timeout is the timeout of the
// prompt's task, it is stored with a resolution of seconds and must be at
// least one second. clearHistory determines whether the agent's history is
// cleared before executing the wakeup (true = clear_ask, false = ask).
func DbAddWakeup(
	agentName string,
	name string,
	description string,
	fireAt *time.Time,
	cronSpec string,
	prompt string,
	timeout time.Duration,
	clearHistory bool,
) error {
	if strings.TrimSpace(agentName) == "" {
		return fmt.Errorf("wakeup %q: missing agent name", name)
	}
	d, err := dbHandle(agentName)
	if err != nil {
		return err
	}
	if err := validateWakeup(name, fireAt, cronSpec, prompt, timeout); err != nil {
		return err
	}

	var fireAtVal any
	if fireAt != nil {
		fireAtVal = fireAt.Unix()
	}

	clearHistoryInt := 0
	if clearHistory {
		clearHistoryInt = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	res, err := d.ExecContext(ctx, insertSQL,
		name, fireAtVal, nullStr(cronSpec),
		prompt, int64(timeout/time.Second), nullStr(description), clearHistoryInt,
	)
	if err != nil {
		return fmt.Errorf("wakeup %q: failed to add: %w", name, err)
	}
	// 0 rows: ON CONFLICT DO NOTHING skipped it, the name is already taken.
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("wakeup %q: %w", name, ErrExists)
	}
	return nil
}

// DbRemoveWakeup deletes a wakeup, missing ones yield ErrNotFound.
func DbRemoveWakeup(agentName, name string) error {
	d, err := dbHandle(agentName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	res, err := d.ExecContext(ctx, deleteSQL, name)
	if err != nil {
		return fmt.Errorf("wakeup %q: failed to delete: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("wakeup %q: %w", name, ErrNotFound)
	}
	return nil
}

type wakeupRow struct {
	name         string
	fireAt       sql.NullInt64
	cronSpec     sql.NullString
	prompt       string
	timeoutSecs  int64
	description  sql.NullString
	clearHistory bool
}

func loadRows(ctx context.Context, d *sql.DB) ([]wakeupRow, error) {
	rows, err := d.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to query wakeups: %w", err)
	}
	defer rows.Close()

	var out []wakeupRow
	for rows.Next() {
		var r wakeupRow
		if err := rows.Scan(&r.name, &r.fireAt, &r.cronSpec, &r.prompt, &r.timeoutSecs, &r.description, &r.clearHistory); err != nil {
			return nil, fmt.Errorf("failed to scan wakeup: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read wakeups: %w", err)
	}
	return out, nil
}

func validateWakeup(
	name string,
	fireAt *time.Time,
	cronSpec string,
	prompt string,
	timeout time.Duration,
) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("wakeup: missing name")
	}
	if (fireAt != nil) == (cronSpec != "") {
		return fmt.Errorf("wakeup %q: exactly one of fire_at or cron_spec must be set", name)
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("wakeup %q: prompt must not be empty", name)
	}
	if timeout < time.Second {
		return fmt.Errorf("wakeup %q: timeout must be at least one second", name)
	}
	return nil
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// newPromptWakeUp builds a wakeup that runs prompt with the given timeout.
// If clearHistory is true, the agent's history is cleared before executing
// the prompt (TaskTypeClearAsk). If false, the prompt is appended to the
// existing history (TaskTypeAsk).
func newPromptWakeUp(name, description string, fireAt time.Time, cronSpec, prompt string, timeout time.Duration, clearHistory bool) *WakeUp {
	var tsk task.Task
	if clearHistory {
		tsk = task.TaskClearAskMultiple(
			[]task.Prompt{{Text: prompt}},
			timeout,
		)
	} else {
		tsk = task.TaskAskSimple(prompt, timeout)
	}
	return NewWakeUp(name, description, fireAt, cronSpec, tsk)
}
