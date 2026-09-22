package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)


const memorySQLTimeout = 5 * time.Second


const defaultSchema = `
CREATE TABLE IF NOT EXISTS table_catalog (
	table_name  TEXT PRIMARY KEY COLLATE NOCASE,
	description TEXT NOT NULL,
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT OR IGNORE INTO table_catalog(table_name, description)
VALUES ('table_catalog', 'Inhaltsverzeichnis: listet alle Tabellen dieser Datenbank mit einer kurzen Beschreibung ihres Zwecks.');
`

var (
	// Matches '...' string literals (with '' as escaped quote), so that keywords
	// inside stored text (e.g. "please attach the file") do not trigger the blocklist.
	memorySQLStringLiteral = regexp.MustCompile(`'(?:[^']|'')*'`)
	// Statements that could reach outside of the agent's own database file.
	// \b does not match inside pragma_table_info(), so that function stays usable in SELECTs.
	memorySQLForbidden = regexp.MustCompile(`(?i)\b(attach|detach|pragma|load_extension|vacuum)\b`)
)

type MemorySQLTool struct {
	maxRows    int
	maxColumns int
	baseSchema string
}

// NewMemorySQLTool constructs a MemorySQLTool, reading all relevant config
// values once up front.
func NewMemorySQLTool() MemorySQLTool {
	cfg := tool.GetToolConfig()

	maxRows := config.ReadEntry(cfg, "memory_sql.max_rows", 100)
	maxColumns := config.ReadEntry(cfg, "memory_sql.max_columns", 50)
	baseSchema := config.ReadEntry(cfg, "memory_sql.base_schema", defaultSchema)

	return MemorySQLTool{
		maxRows:    maxRows,
		maxColumns: maxColumns,
		baseSchema: baseSchema,
	}
}

func (MemorySQLTool) Name() string {
	return "memory_sql"
}



func (MemorySQLTool) Description() string {
	return "Your persistent memory: a private SQLite database. " +
		"IMPORTANT: If you pass multiple statements separated by ';', ALL of them are executed, " +
		"but the output and 'rows_affected' will ONLY show the result of the VERY LAST statement. " +
		"There is exactly one base table, 'table_catalog' (table_name, description, updated_at), which acts as a table of contents " +
		"for this database: it lists every other table you create, along with a short description of what it's for. " +
		"updated_at is stored as an ISO8601 UTC string (e.g. '2026-09-22T14:30:00Z'), not a unix timestamp — " +
		"use strftime('%Y-%m-%dT%H:%M:%SZ', 'now') as the default for any timestamp column you add to your own tables too. " +
		"Discover the schema with: SELECT * FROM table_catalog; or SELECT name, sql FROM sqlite_master WHERE type='table'; " +
		"or SELECT * FROM pragma_table_info('table_name');. " +
		"IMPORTANT: Whenever you CREATE a new table, you MUST also INSERT a row into table_catalog describing its purpose. " +
		"Whenever you DROP a table, you MUST also DELETE its corresponding row from table_catalog. " +
		"This is NOT enforced by the tool — keeping table_catalog accurate and up to date is entirely your responsibility. " +
		"Upsert a catalog entry with: INSERT INTO table_catalog(table_name, description) VALUES ('my_table', 'what it stores and why') " +
		"ON CONFLICT(table_name) DO UPDATE SET description = excluded.description, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'). " +
		"Escape single quotes in strings by doubling them (it''s). Never use double quotes for string literals. " +
		"ATTACH, PRAGMA, VACUUM and LOAD_EXTENSION statements are forbidden."
}


func (MemorySQLTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{
				"type":        "string",
				"description": "The single SQL command to execute (e.g., SELECT, INSERT, CREATE TABLE).",
			},
		},
		"required":             []string{"sql"},
		"additionalProperties": false,
	}
}

func (m MemorySQLTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		agentName := strings.TrimSpace(handle.Name())
		if agentName == "" {
			return "", fmt.Errorf("memory_sql: agent handle returned empty name")
		}

		// filepath.Base strips any path components (path traversal protection).
		fileName := filepath.Base(agentName)
		if fileName == "." || fileName == ".." || fileName == string(os.PathSeparator) {
			return "", fmt.Errorf("memory_sql: invalid agent name %q", agentName)
		}
		dbPath := filepath.Join(constants.MemoryFolderPath, fileName+".db")

		var args struct {
			SQL string `json:"sql"`
		}
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("memory_sql: invalid arguments: %w", err)
			}
		}

		if err := memorySQLCheck(args.SQL); err != nil {
			return "", err
		}

		ctx, cancel := context.WithTimeout(ctx, memorySQLTimeout)
		defer cancel()

		db, err := openMemoryDB(ctx, m.baseSchema, dbPath)
		if err != nil {
			return "", fmt.Errorf("memory_sql: %w", err)
		}
		defer db.Close()

		return m.memorySQLExecute(ctx, db, args.SQL)
	}
}

// ---------------------------------------------------------------------------
// Database handling
// ---------------------------------------------------------------------------

type memoryDB struct {
	rw *sql.DB // read/write connection
	ro *sql.DB // connection with query_only, used for queries
}

func (d *memoryDB) Close() {
	d.ro.Close()
	d.rw.Close()
}

func openMemoryDB(ctx context.Context, baseSchema, path string) (*memoryDB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("could not create memory directory: %w", err)
	}

	rw, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	rw.SetMaxOpenConns(1)

	// Creates the file on first use and makes sure the base tables exist.
	if _, err := rw.ExecContext(ctx, baseSchema); err != nil {
		rw.Close()
		return nil, fmt.Errorf("could not initialise database: %w", err)
	}

	ro, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		rw.Close()
		return nil, fmt.Errorf("could not open read-only connection: %w", err)
	}
	return &memoryDB{rw: rw, ro: ro}, nil
}

// memorySQLCheck rejects empty statements and statements that could leave the agent's own database.
func memorySQLCheck(stmt string) error {
	if strings.TrimSpace(stmt) == "" {
		return fmt.Errorf("memory_sql: 'sql' is required")
	}
	stripped := memorySQLStringLiteral.ReplaceAllString(stmt, "''")
	if kw := memorySQLForbidden.FindString(stripped); kw != "" {
		return fmt.Errorf("memory_sql: keyword %q is not allowed", strings.ToUpper(kw))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Result handling
// ---------------------------------------------------------------------------

type memorySQLResult struct {
	Columns          []string `json:"columns"`
	Rows             [][]any  `json:"rows"`
	Truncated        bool     `json:"truncated,omitempty"`
	ColumnsTruncated bool     `json:"columns_truncated,omitempty"`
}

func memorySQLSelect(ctx context.Context, db *sql.DB, maxRows, maxCols int, query string, args ...any) (*memorySQLResult, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	columnsTruncated := false
	if len(cols) > maxCols {
		cols = cols[:maxCols]
		columnsTruncated = true
	}

	res := &memorySQLResult{
		Columns:          cols,
		Rows:             [][]any{},
		ColumnsTruncated: columnsTruncated,
	}

	for rows.Next() {
		if len(res.Rows) >= maxRows {
			res.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = fmt.Sprintf("<blob, %d bytes>", len(b))
			}
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

func memorySQLMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("memory_sql: failed to marshal result: %w", err)
	}
	return string(b), nil
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

// isSelectQuery checks if the statement is a read-only query to route it to the ro connection.
func isSelectQuery(sql string) bool {
	s := strings.ToUpper(strings.TrimSpace(sql))
	return strings.HasPrefix(s, "SELECT") ||
		strings.HasPrefix(s, "WITH") ||
		strings.HasPrefix(s, "VALUES") ||
		strings.HasPrefix(s, "EXPLAIN")
}

func (m MemorySQLTool) memorySQLExecute(ctx context.Context, db *memoryDB, query string) (string, error) {
	if isSelectQuery(query) {
		res, err := memorySQLSelect(ctx, db.ro, m.maxRows, m.maxColumns, query)
		if err != nil {
			return "", fmt.Errorf("memory_sql: query failed: %w", err)
		}
		return memorySQLMarshal(res)
	}

	// For non-SELECT queries (INSERT, UPDATE, CREATE, etc.), use the read/write connection
	res, err := db.rw.ExecContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("memory_sql: exec failed: %w", err)
	}

	affected, _ := res.RowsAffected()
	var lastID int64
	if id, err := res.LastInsertId(); err == nil {
		lastID = id
	}

	return memorySQLMarshal(struct {
		OK           bool  `json:"ok"`
		RowsAffected int64 `json:"rows_affected"`
		LastInsertID int64 `json:"last_insert_id,omitempty"`
	}{true, affected, lastID})
}
