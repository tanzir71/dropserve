package sqlitebrowser

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBrowseListsTablesCapsRowsAndReleasesWriteLock(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "catalog.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open SQLite fixture: %v", err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL); CREATE TABLE notes (body TEXT)`); err != nil {
		t.Fatalf("create SQLite fixture tables: %v", err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin SQLite fixture rows: %v", err)
	}
	for index := 1; index <= 125; index++ {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO items(id, name) VALUES (?, ?)`, index, fmt.Sprintf("item-%03d", index)); err != nil {
			t.Fatalf("insert SQLite fixture row %d: %v", index, err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO notes(body) VALUES ('fixture note')`); err != nil {
		t.Fatalf("insert SQLite note: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit SQLite fixture: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close SQLite fixture writer: %v", err)
	}

	snapshot, err := Browse(ctx, databasePath)
	if err != nil {
		t.Fatalf("browse SQLite fixture: %v", err)
	}
	if len(snapshot.Tables) != 2 || snapshot.Tables[0].Name != "items" || snapshot.Tables[1].Name != "notes" {
		t.Fatalf("SQLite tables = %#v", snapshot.Tables)
	}
	items := snapshot.Tables[0]
	if len(items.Columns) != 2 || items.Columns[0] != "id" || items.Columns[1] != "name" {
		t.Fatalf("items columns = %v", items.Columns)
	}
	if len(items.Rows) != 100 {
		t.Fatalf("items row count = %d, want first 100", len(items.Rows))
	}
	if got := items.Rows[0][1]; got != "item-001" {
		t.Fatalf("first item = %#v", got)
	}
	if got := items.Rows[99][1]; got != "item-100" {
		t.Fatalf("hundredth item = %#v", got)
	}

	writer, err := sql.Open("sqlite", databasePath+"?_pragma=busy_timeout(250)")
	if err != nil {
		t.Fatalf("reopen SQLite fixture for writing: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.ExecContext(ctx, `BEGIN IMMEDIATE; UPDATE notes SET body = 'writer was not blocked'; COMMIT`); err != nil {
		t.Fatalf("browser retained a write lock: %v", err)
	}
}
