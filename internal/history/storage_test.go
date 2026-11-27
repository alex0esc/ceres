package history

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSaveLoadRoundTrip(t *testing.T) {
	db := newTestDB(t)
	if err := initialiseSchema(db); err != nil {
		t.Fatal(err)
	}

	want := History{
		TotalTokens: 1234,
		Entries: []Entry{
			{Type: EntryTypeUser, Text: "hello", Timestamp: 1000},
			{Type: EntryTypeAssistant, Text: "world", Timestamp: 1001},
			{
				Type:      EntryTypeReasoning,
				Reasoning: &Reasoning{ID: "r1", Summary: []string{"thinking"}, Content: []string{"more"}},
				Timestamp: 1002,
			},
			{
				Type:      EntryTypeToolCall,
				ToolCall:  &ToolCall{Name: "bash", Arguments: `{"cmd":"ls"}`, CallID: "c1"},
				Timestamp: 1003,
			},
			{
				Type:       EntryTypeToolResult,
				ToolResult: &ToolResult{CallID: "c1", Output: "ok"},
				Timestamp:  1004,
			},
			{
				Type:      EntryTypeImage,
				Images:    []Image{{Base64: "YWJj", MimeType: "image/png", Detail: "auto"}},
				Timestamp: 1005,
			},
		},
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveHistory(tx, want); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries, want.Entries) {
		t.Fatalf("entries mismatch:\n got: %#v\nwant: %#v", entries, want.Entries)
	}

	var tokens int64
	if err := db.QueryRow(metaSelect).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != want.TotalTokens {
		t.Fatalf("tokens = %d, want %d", tokens, want.TotalTokens)
	}
}

func TestMigrateLegacyBlob(t *testing.T) {
	db := newTestDB(t)
	const legacySchema = `
CREATE TABLE histories (
	id         INTEGER PRIMARY KEY CHECK (id = 1),
	history    TEXT NOT NULL,
	updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}

	legacy := History{
		TotalTokens: 77,
		Entries: []Entry{
			{Type: EntryTypeUser, Text: "old message", Timestamp: 42},
			{Type: EntryTypeAssistant, Text: "old reply", Timestamp: 43},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO histories (id, history) VALUES (1, ?)`, string(data)); err != nil {
		t.Fatal(err)
	}

	if err := initialiseSchema(db); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries, legacy.Entries) {
		t.Fatalf("migrated entries mismatch:\n got: %#v\nwant: %#v", entries, legacy.Entries)
	}
	var tokens int64
	if err := db.QueryRow(metaSelect).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != legacy.TotalTokens {
		t.Fatalf("tokens = %d, want %d", tokens, legacy.TotalTokens)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'histories'`).Scan(&name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("legacy table still present (scan error: %v)", err)
	}
}
