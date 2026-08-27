package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestDashboardAtRoot(t *testing.T) {
	t.Parallel()

	server, err := dropserver.New(scanner.Options{Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200; body=%q", result.StatusCode, body)
	}
	if contentType := result.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	for _, expected := range []string{"<title>Dropserve</title>", `id="app-search"`, `autofocus`} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("dashboard body does not contain %q: %s", expected, body)
		}
	}
}

func TestSearchFindsREADMEContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDashboardFixture(t, root, "atlas", "Atlas", "A quiet tool.")
	writeDashboardFixture(t, root, "beacon", "Beacon", "Contains the copperlantern field guide for night work.")
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create search server: %v", err)
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/_dropserve/api/search?q=copperlantern",
		nil,
	)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	if result.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(result.Body)
		t.Fatalf("search API returned %d, want 200; body=%q", result.StatusCode, body)
	}
	var entries []struct {
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(result.Body).Decode(&entries); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "beacon" {
		t.Fatalf("README-only search results = %#v, want beacon only", entries)
	}
	if !strings.Contains(entries[0].Description, "copperlantern") {
		t.Fatalf("indexed description = %q, want README paragraph", entries[0].Description)
	}
}

func writeDashboardFixture(t *testing.T, root, name, title, readmeParagraph string) {
	t.Helper()

	appRoot := filepath.Join(root, name)
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create %s fixture: %v", name, err)
	}
	index := "<!doctype html><title>" + title + "</title><h1>" + title + "</h1>"
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatalf("write %s index: %v", name, err)
	}
	readme := "# " + title + "\n\n" + readmeParagraph + "\n"
	if err := os.WriteFile(filepath.Join(appRoot, "README.md"), []byte(readme), 0o600); err != nil {
		t.Fatalf("write %s README: %v", name, err)
	}
}

func TestAppsAPIListsEveryFixture(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate fixtures")
	}
	fixtures := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "testdata", "fixtures"))
	server, err := dropserver.New(scanner.Options{Roots: []string{fixtures}})
	if err != nil {
		t.Fatalf("create fixture server: %v", err)
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/_dropserve/api/apps",
		nil,
	)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	if result.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(result.Body)
		t.Fatalf("apps API returned %d, want 200; body=%q", result.StatusCode, body)
	}
	if contentType := result.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var entries []struct {
		Slug   string `json:"slug"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Status string `json:"status"`
		URLs   struct {
			Path string `json:"path"`
		} `json:"urls"`
	}
	if err := json.NewDecoder(result.Body).Decode(&entries); err != nil {
		t.Fatalf("decode apps API: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("apps API returned %d entries, want every fixture (1): %#v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Slug != "static" || entry.Name != "static" || entry.Type != "static" || entry.Status != "ready" {
		t.Fatalf("static fixture metadata is incorrect: %#v", entry)
	}
	if entry.URLs.Path != "/static/" {
		t.Fatalf("static fixture path URL = %q, want /static/", entry.URLs.Path)
	}
}
