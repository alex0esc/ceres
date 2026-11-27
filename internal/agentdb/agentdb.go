// Package agentdb owns the SQLite handles of one agent.
//
// Every agent has two database files:
//
//   - database/<agent>/persistent_data.db — chat history and wakeups
//   - database/<agent>/memories.db        — the memory_sql tool
//
// Both files are opened exactly once per agent and shared by all packages that
// need them. The schemas themselves stay with their owners (history, wakeup,
// memory_sql); this package only manages connections, pragmas and shutdown.
package agentdb

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/alex0esc/ceres/internal/constants"
)

// DBs holds the open handles of one agent. MemoryRW and MemoryRO point at the
// same file: RO is opened with query_only(1) so a misclassified SELECT can
// never write to the agent's memory.
type DBs struct {
	Persistent *sql.DB
	MemoryRW   *sql.DB
	MemoryRO   *sql.DB
}

var (
	mu  sync.Mutex
	dbs = make(map[string]*DBs)
)

// Open returns the handles for agentName, opening them on first use. Subsequent
// calls return the same handles. Callers must not close individual handles;
// CloseAll does that at shutdown.
func Open(agentName string) (*DBs, error) {
	mu.Lock()
	defer mu.Unlock()

	if d, ok := dbs[agentName]; ok {
		return d, nil
	}

	persistentPath := constants.AgentPersistentDBPath(agentName)
	memoryPath := constants.AgentMemoriesDBPath(agentName)
	for _, path := range []string{persistentPath, memoryPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("agentdb: failed to create directory for %q: %w", agentName, err)
		}
	}

	persistent, err := sql.Open("sqlite", persistentPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("agentdb: failed to open persistent database for %q: %w", agentName, err)
	}
	// Wakeup operations already relied on serialized writes. Keeping a single
	// connection also avoids SQLITE_BUSY between history and wakeup writes.
	persistent.SetMaxOpenConns(1)

	memoryRW, err := sql.Open("sqlite", memoryPath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		persistent.Close()
		return nil, fmt.Errorf("agentdb: failed to open memory database for %q: %w", agentName, err)
	}
	memoryRW.SetMaxOpenConns(1)

	memoryRO, err := sql.Open("sqlite", memoryPath+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		memoryRW.Close()
		persistent.Close()
		return nil, fmt.Errorf("agentdb: failed to open read-only memory database for %q: %w", agentName, err)
	}

	d := &DBs{Persistent: persistent, MemoryRW: memoryRW, MemoryRO: memoryRO}
	dbs[agentName] = d
	return d, nil
}

// Get returns the already-open handles for agentName. An unknown name is a
// programming error and terminates the process, just like app.GetAgent does.
func Get(agentName string) *DBs {
	mu.Lock()
	defer mu.Unlock()

	d, ok := dbs[agentName]
	if !ok {
		log.Fatalf("unknown agent %s", agentName)
	}
	return d
}

// CloseAll closes every handle opened through Open.
func CloseAll() {
	mu.Lock()
	defer mu.Unlock()

	for name, d := range dbs {
		d.MemoryRO.Close()
		d.MemoryRW.Close()
		d.Persistent.Close()
		delete(dbs, name)
	}
}
