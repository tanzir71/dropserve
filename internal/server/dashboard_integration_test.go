package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/indexer"
	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
	"github.com/tanzir71/dropserve/internal/supervisor"
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

func TestServerPersistsIndexOutsideAppRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDashboardFixture(t, root, "ledger", "Ledger", "Tracks local records.")
	stateDirectory := t.TempDir()
	indexPath := filepath.Join(stateDirectory, "index.json")
	_, err := dropserver.NewWithOptions(dropserver.Options{
		Scanner:   scanner.Options{Roots: []string{root}},
		IndexPath: indexPath,
	})
	if err != nil {
		t.Fatalf("create persistent server: %v", err)
	}
	entries, err := indexer.Load(indexPath)
	if err != nil {
		t.Fatalf("load persisted server index: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "ledger" {
		t.Fatalf("persisted server index = %#v, want ledger", entries)
	}
	appEntries, err := os.ReadDir(filepath.Join(root, "ledger"))
	if err != nil {
		t.Fatalf("read app root: %v", err)
	}
	if len(appEntries) != 2 {
		t.Fatalf("server wrote inside app root: %v", appEntries)
	}
	stateEntries, err := os.ReadDir(stateDirectory)
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	if len(stateEntries) != 1 || stateEntries[0].Name() != "index.json" {
		t.Fatalf("state directory contents = %v, want only index.json", stateEntries)
	}
}

func TestDashboardHandlesZeroAndTwoHundredApps(t *testing.T) {
	t.Parallel()

	emptyServer, err := dropserver.New(scanner.Options{Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("create empty dashboard: %v", err)
	}
	emptyEntries := fetchAppSlugs(t, emptyServer.Handler())
	if emptyEntries == nil || len(emptyEntries) != 0 {
		t.Fatalf("empty dashboard entries = %#v, want non-nil empty list", emptyEntries)
	}

	root := t.TempDir()
	for index := range 200 {
		name := fmt.Sprintf("app-%03d", index)
		writeDashboardFixture(t, root, name, name, "Scale fixture.")
	}
	largeServer, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create 200-app dashboard: %v", err)
	}
	largeEntries := fetchAppSlugs(t, largeServer.Handler())
	if len(largeEntries) != 200 {
		t.Fatalf("large dashboard returned %d apps, want 200", len(largeEntries))
	}
	unique := make(map[string]struct{}, len(largeEntries))
	for _, slug := range largeEntries {
		unique[slug] = struct{}{}
	}
	if len(unique) != 200 {
		t.Fatalf("large dashboard returned %d unique slugs, want 200", len(unique))
	}
}

func TestDropserveNamespaceCannotBeShadowed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDashboardFixture(t, root, "_dropserve", "Impostor", "This app must never mount.")
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create reserved-route server: %v", err)
	}

	apps := fetchAppSlugs(t, server.Handler())
	if len(apps) != 0 {
		t.Fatalf("reserved app appeared in API: %#v", apps)
	}
	for _, requestPath := range []string{"/", "/_dropserve/app.css", "/_dropserve/api/apps"} {
		request := httptest.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"http://dropserve.test"+requestPath,
			nil,
		)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		result := response.Result()
		body, readErr := io.ReadAll(result.Body)
		_ = result.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", requestPath, readErr)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("system route %s returned %d, want 200", requestPath, result.StatusCode)
		}
		if bytes.Contains(body, []byte("Impostor")) {
			t.Fatalf("reserved app shadowed system route %s: %q", requestPath, body)
		}
	}
}

func TestDashboardReadOnlyOperationalAPIs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDashboardFixture(t, root, "notes", "Notes", "Personal notes.")
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create operational API server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	healthResponse := getWithContext(t, httpServer.Client(), httpServer.URL+"/_dropserve/healthz")
	healthBody, readErr := io.ReadAll(healthResponse.Body)
	_ = healthResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read health: %v", readErr)
	}
	if healthResponse.StatusCode != http.StatusOK || string(healthBody) != "ok\n" {
		t.Fatalf("health response = %d %q, want 200 ok", healthResponse.StatusCode, healthBody)
	}

	statusResponse := getWithContext(t, httpServer.Client(), httpServer.URL+"/_dropserve/api/status")
	var status struct {
		Version       string `json:"version"`
		UptimeSeconds int64  `json:"uptime_seconds"`
		CSRFToken     string `json:"csrf_token"`
		Ports         struct {
			HTTP int `json:"http"`
		} `json:"ports"`
		Warnings []any `json:"warnings"`
	}
	decodeErr := json.NewDecoder(statusResponse.Body).Decode(&status)
	_ = statusResponse.Body.Close()
	if decodeErr != nil {
		t.Fatalf("decode status: %v", decodeErr)
	}
	if statusResponse.StatusCode != http.StatusOK || status.Version == "" || status.UptimeSeconds < 0 || len(status.CSRFToken) < 32 || status.Ports.HTTP == 0 || status.Warnings == nil {
		t.Fatalf("status payload is incomplete: %#v", status)
	}

	detailResponse := getWithContext(t, httpServer.Client(), httpServer.URL+"/_dropserve/api/apps/notes")
	var detail struct {
		Slug      string   `json:"slug"`
		Path      string   `json:"path"`
		Detection string   `json:"detection"`
		Warnings  []string `json:"warnings"`
	}
	decodeErr = json.NewDecoder(detailResponse.Body).Decode(&detail)
	_ = detailResponse.Body.Close()
	if decodeErr != nil {
		t.Fatalf("decode app detail: %v", decodeErr)
	}
	if detailResponse.StatusCode != http.StatusOK || detail.Slug != "notes" || detail.Path != filepath.Join(root, "notes") || detail.Detection == "" || detail.Warnings == nil {
		t.Fatalf("app detail is incomplete: status=%d detail=%#v", detailResponse.StatusCode, detail)
	}

	missingResponse := getWithContext(t, httpServer.Client(), httpServer.URL+"/_dropserve/api/apps/missing")
	_ = missingResponse.Body.Close()
	if missingResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("missing app detail returned %d, want 404", missingResponse.StatusCode)
	}
}

func getWithContext(t *testing.T, client *http.Client, targetURL string) *http.Response {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, targetURL, nil)
	if err != nil {
		t.Fatalf("create request for %s: %v", targetURL, err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("fetch %s: %v", targetURL, err)
	}
	return response
}

func fetchAppSlugs(t *testing.T, handler http.Handler) []string {
	t.Helper()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/_dropserve/api/apps",
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	var entries []struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(result.Body).Decode(&entries); err != nil {
		t.Fatalf("decode dashboard entries: %v", err)
	}
	if entries == nil {
		t.Fatal("dashboard apps API encoded null, want an empty JSON list")
	}
	slugs := make([]string, len(entries))
	for index, entry := range entries {
		slugs[index] = entry.Slug
	}
	return slugs
}

func TestQREndpointReturnsPNG(t *testing.T) {
	t.Parallel()

	server, err := dropserver.New(scanner.Options{Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("create QR server: %v", err)
	}
	firstURL := "http://example.test/static/"
	first := requestQR(t, server.Handler(), firstURL)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("QR endpoint returned %d, want 200; body=%q", first.StatusCode, first.Body)
	}
	if contentType := first.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("QR Content-Type = %q, want image/png", contentType)
	}
	if !bytes.HasPrefix(first.Body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("QR response does not have PNG signature: %x", first.Body[:min(len(first.Body), 16)])
	}
	configuration, err := png.DecodeConfig(bytes.NewReader(first.Body))
	if err != nil {
		t.Fatalf("decode QR PNG: %v", err)
	}
	if configuration.Width < 128 || configuration.Height < 128 || len(first.Body) < 300 {
		t.Fatalf("QR PNG is not substantial: %dx%d, %d bytes", configuration.Width, configuration.Height, len(first.Body))
	}

	second := requestQR(t, server.Handler(), "http://example.test/another-app/")
	if bytes.Equal(first.Body, second.Body) {
		t.Fatal("different URLs produced identical QR PNGs")
	}
}

type qrResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func requestQR(t *testing.T, handler http.Handler, targetURL string) qrResponse {
	t.Helper()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/_dropserve/api/qr?url="+url.QueryEscape(targetURL),
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read QR response: %v", err)
	}
	return qrResponse{StatusCode: result.StatusCode, Header: result.Header, Body: body}
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

func TestSearchFindsFilename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDashboardFixture(t, root, "atlas", "Atlas", "A quiet tool.")
	writeDashboardFixture(t, root, "beacon", "Beacon", "A bright tool.")
	templates := filepath.Join(root, "beacon", "templates")
	if err := os.Mkdir(templates, 0o750); err != nil {
		t.Fatalf("create templates folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templates, "copperneedle-invoice.html"), []byte("invoice"), 0o600); err != nil {
		t.Fatalf("write searchable filename: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create search server: %v", err)
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/_dropserve/api/search?q=copperneedle",
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
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(result.Body).Decode(&entries); err != nil {
		t.Fatalf("decode filename search: %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "beacon" {
		t.Fatalf("filename-only search results = %#v, want beacon only", entries)
	}
}

func TestSearchRanksNameAboveFilename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDashboardFixture(t, root, "invoice-studio", "Invoice Studio", "Create documents.")
	writeDashboardFixture(t, root, "archive", "Archive", "Stored documents.")
	if err := os.WriteFile(filepath.Join(root, "archive", "invoice-history.txt"), []byte("history"), 0o600); err != nil {
		t.Fatalf("write filename match: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create ranking server: %v", err)
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/_dropserve/api/search?q=invoice",
		nil,
	)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	var entries []struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(result.Body).Decode(&entries); err != nil {
		t.Fatalf("decode ranked search: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ranked search returned %d entries, want 2: %#v", len(entries), entries)
	}
	if entries[0].Slug != "invoice-studio" || entries[1].Slug != "archive" {
		t.Fatalf("ranked search order = %#v, want name match before filename match", entries)
	}
}

func TestEveryAdvertisedURLWorks(t *testing.T) {
	t.Parallel()

	server, err := dropserver.New(scanner.Options{Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("create URL server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/_dropserve/api/urls",
		nil,
	)
	if err != nil {
		t.Fatalf("create URLs request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("fetch advertised URLs: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("URLs API returned %d, want 200; body=%q", response.StatusCode, body)
	}
	var advertised []struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&advertised); err != nil {
		t.Fatalf("decode advertised URLs: %v", err)
	}
	if len(advertised) == 0 {
		t.Fatal("URLs API advertised nothing")
	}
	for _, item := range advertised {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, item.URL, nil)
		if err != nil {
			t.Fatalf("create request for advertised %s URL %q: %v", item.Kind, item.URL, err)
		}
		result, err := httpServer.Client().Do(request)
		if err != nil {
			t.Fatalf("fetch advertised %s URL %q: %v", item.Kind, item.URL, err)
		}
		_ = result.Body.Close()
		if result.StatusCode >= http.StatusBadRequest {
			t.Fatalf("advertised %s URL %q returned %d, want < 400", item.Kind, item.URL, result.StatusCode)
		}
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
	server, err := dropserver.NewWithOptions(dropserver.Options{
		Scanner: scanner.Options{Roots: []string{fixtures}},
		Supervisor: supervisor.Options{
			RestartDelays: []time.Duration{10 * time.Millisecond},
		},
	})
	if err != nil {
		t.Fatalf("create fixture server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close fixture server: %v", closeErr)
		}
	}()
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
	expected := map[string]struct {
		name   string
		kind   string
		status string
	}{
		"broken":        {name: "broken", kind: "command", status: "crashed"},
		"field-notes":   {name: "field notes", kind: "static", status: "ready"},
		"invoice-desk":  {name: "invoice desk", kind: "static", status: "ready"},
		"kitchen-timer": {name: "kitchen timer", kind: "static", status: "ready"},
		"node":          {name: "node", kind: "command", status: "ready"},
		"python":        {name: "python", kind: "command", status: "ready"},
		"static":        {name: "static", kind: "static", status: "ready"},
	}
	if len(entries) != len(expected) {
		t.Fatalf("apps API returned %d entries, want every fixture (%d): %#v", len(entries), len(expected), entries)
	}
	for _, entry := range entries {
		want, found := expected[entry.Slug]
		if !found || entry.Name != want.name || entry.Type != want.kind || entry.Status != want.status {
			t.Fatalf("fixture metadata is incorrect: %#v", entry)
		}
		if entry.URLs.Path != "/"+entry.Slug+"/" {
			t.Fatalf("%s path URL = %q, want /%s/", entry.Slug, entry.URLs.Path, entry.Slug)
		}
	}
}
