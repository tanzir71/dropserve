package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestHotConfigurationRebuildsAppsAndKeepsLastGoodSnapshot(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeReloadApp(t, firstRoot, "first", "first app")
	writeReloadApp(t, secondRoot, "second", "second app")
	server, err := dropserver.NewWithOptions(dropserver.Options{Scanner: scanner.Options{Roots: []string{firstRoot}}, DashboardTitle: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if err := server.UpdateConfiguration(scanner.Options{Roots: []string{secondRoot}}, "After", "dark", "second", 7500, 7600); err != nil {
		t.Fatalf("apply valid hot config: %v", err)
	}
	assertReloadBody(t, server, "/", "second app")
	assertReloadBody(t, server, "/_dropserve/", "<title>After</title>")
	entries := reloadEntries(t, server)
	if len(entries) != 1 || entries[0].Slug != "second" {
		t.Fatalf("hot-reloaded entries = %#v", entries)
	}

	missingRegisteredApp := filepath.Join(t.TempDir(), "missing-app")
	if err := server.UpdateConfiguration(scanner.Options{Registered: []string{missingRegisteredApp}}, "Broken", "light", "", 7700, 7800); err == nil {
		t.Fatal("invalid hot config unexpectedly succeeded")
	}
	assertReloadBody(t, server, "/", "second app")
	assertReloadBody(t, server, "/_dropserve/", "<title>After</title>")
}

func writeReloadApp(t *testing.T, root, name, body string) {
	t.Helper()
	appRoot := filepath.Join(root, name)
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertReloadBody(t *testing.T, server *dropserver.Server, path, want string) {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test"+path, nil)
	request.RemoteAddr = "127.0.0.1:5000"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
		t.Fatalf("GET %s = %d %q, want %q", path, response.Code, response.Body.String(), want)
	}
}

func reloadEntries(t *testing.T, server *dropserver.Server) []indexer.Entry {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/api/apps", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var entries []indexer.Entry
	if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	return entries
}
