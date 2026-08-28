package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
	"github.com/tanzir71/dropserve/internal/sqlitebrowser"
	_ "modernc.org/sqlite"
)

func TestDiscoveredSQLiteDatabaseIsBrowsableFromDashboard(t *testing.T) {
	ctx := context.Background()
	appsRoot := t.TempDir()
	appRoot := filepath.Join(appsRoot, "inventory")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create SQLite app fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte("<!doctype html><title>Inventory</title>"), 0o600); err != nil {
		t.Fatalf("write SQLite app index: %v", err)
	}
	databasePath := filepath.Join(appRoot, "catalog.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open SQLite app fixture: %v", err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT);`); err != nil {
		t.Fatalf("create product table: %v", err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin product fixture: %v", err)
	}
	for index := 1; index <= 105; index++ {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO products(id, name) VALUES (?, ?)`, index, fmt.Sprintf("product-%03d", index)); err != nil {
			t.Fatalf("insert product %d: %v", index, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit products: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close SQLite app fixture: %v", err)
	}

	server, err := dropserver.New(scanner.Options{Roots: []string{appsRoot}})
	if err != nil {
		t.Fatalf("start server with SQLite app: %v", err)
	}
	defer func() { _ = server.Close() }()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/_dropserve/api/databases/inventory?file="+url.QueryEscape("catalog.db"), nil)
	if err != nil {
		t.Fatalf("create SQLite dashboard request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("browse SQLite database through dashboard: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SQLite dashboard response = %d, want 200", response.StatusCode)
	}
	var snapshot sqlitebrowser.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode SQLite dashboard response: %v", err)
	}
	if len(snapshot.Tables) != 1 || snapshot.Tables[0].Name != "products" || len(snapshot.Tables[0].Rows) != 100 {
		t.Fatalf("SQLite dashboard snapshot = %#v", snapshot)
	}

	writer, err := sql.Open("sqlite", databasePath+"?_pragma=busy_timeout(250)")
	if err != nil {
		t.Fatalf("reopen app database: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.ExecContext(ctx, `UPDATE products SET name = 'still writable' WHERE id = 1`); err != nil {
		t.Fatalf("dashboard browser retained database lock: %v", err)
	}
}
