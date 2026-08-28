package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestDashboardPreferencesPersistAndSortByPinThenLastUse(t *testing.T) {
	root := t.TempDir()
	writeReloadApp(t, root, "alpha", "alpha")
	writeReloadApp(t, root, "beta", "beta")
	older := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(root, "alpha", "index.html"), older, older); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "index.json")
	options := dropserver.Options{Scanner: scanner.Options{Roots: []string{root}}, IndexPath: indexPath}
	server, err := dropserver.NewWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/alpha/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	deadline := time.Now().Add(3 * time.Second)
	for {
		entries := preferenceEntries(t, server, true)
		if len(entries) == 2 && entries[0].Slug == "alpha" && entries[0].LastUsed != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last-used ordering did not appear: %#v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}

	postPreference(t, server, "beta", `{"pinned":true}`)
	entries := preferenceEntries(t, server, true)
	if entries[0].Slug != "beta" || !entries[0].Pinned {
		t.Fatalf("pinned ordering = %#v", entries)
	}
	postPreference(t, server, "beta", `{"hidden":true}`)
	if visible := preferenceEntries(t, server, false); len(visible) != 1 || visible[0].Slug != "alpha" {
		t.Fatalf("visible entries after hide = %#v", visible)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close first server: %v", err)
	}

	reloaded, err := dropserver.NewWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })
	entries = preferenceEntries(t, reloaded, true)
	if len(entries) != 2 || entries[0].Slug != "beta" || !entries[0].Pinned || !entries[0].Hidden || entries[1].Slug != "alpha" || entries[1].LastUsed == 0 {
		t.Fatalf("reloaded preferences = %#v", entries)
	}
}

func postPreference(t *testing.T, server *dropserver.Server, slug, body string) {
	t.Helper()
	statusRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/_dropserve/api/status", nil)
	statusResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusResponse, statusRequest)
	var status struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://dropserve.test/_dropserve/api/apps/"+slug+"/settings", bytes.NewBufferString(body))
	request.Header.Set("Origin", "http://dropserve.test")
	request.Header.Set("X-Dropserve-CSRF", status.CSRFToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("POST settings = %d: %s", response.Code, response.Body.String())
	}
}

func preferenceEntries(t *testing.T, server *dropserver.Server, includeHidden bool) []indexer.Entry {
	t.Helper()
	path := "/_dropserve/api/apps"
	if includeHidden {
		path += "?include_hidden=1"
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test"+path, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET preferences = %d: %s", response.Code, response.Body.String())
	}
	var entries []indexer.Entry
	if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode preferences: %v; body=%s", err, response.Body.String())
	}
	return entries
}
