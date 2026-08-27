package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestAbsolutePathsFixturePrefersAndServesOwnPort(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the absolute-paths fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "absolute-paths"))
	if err != nil {
		t.Fatalf("resolve absolute-paths fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create absolute-paths server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close absolute-paths server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	detailRequest, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/_dropserve/api/apps/absolute-paths",
		nil,
	)
	if err != nil {
		t.Fatalf("create absolute-paths detail request: %v", err)
	}
	detailResponse, err := httpServer.Client().Do(detailRequest)
	if err != nil {
		t.Fatalf("request absolute-paths detail: %v", err)
	}
	defer func() {
		_ = detailResponse.Body.Close()
	}()
	var detail struct {
		Port           int  `json:"port"`
		PrefersOwnPort bool `json:"prefers_own_port"`
		URLs           struct {
			Own string `json:"own"`
		} `json:"urls"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
		t.Fatalf("decode absolute-paths detail: %v", err)
	}
	if !detail.PrefersOwnPort || detail.Port == 0 {
		t.Fatalf("absolute-paths routing metadata = %#v", detail)
	}
	wantOwnURL := "http://127.0.0.1:" + strconv.Itoa(detail.Port) + "/"
	if detail.URLs.Own != wantOwnURL {
		t.Fatalf("own-port URL = %q, want %q", detail.URLs.Own, wantOwnURL)
	}
	directRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, wantOwnURL, nil)
	if err != nil {
		t.Fatalf("create own-port request: %v", err)
	}
	directResponse, err := httpServer.Client().Do(directRequest)
	if err != nil {
		t.Fatalf("request own port: %v", err)
	}
	defer func() {
		_ = directResponse.Body.Close()
	}()
	body, err := io.ReadAll(directResponse.Body)
	if err != nil {
		t.Fatalf("read own-port response: %v", err)
	}
	if directResponse.StatusCode != http.StatusOK || string(body) != `<!doctype html><html><head><title>Absolute paths</title></head><body><script src="/app.js"></script><h1>Absolute paths fixture</h1></body></html>` {
		t.Fatalf("own-port response = %d %q", directResponse.StatusCode, body)
	}
}

func TestAssignedCommandPortPersistsAcrossDropserveRestart(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the Node fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "node"))
	if err != nil {
		t.Fatalf("resolve Node fixture: %v", err)
	}
	options := dropserver.Options{
		Scanner:   scanner.Options{Registered: []string{fixture}},
		IndexPath: filepath.Join(t.TempDir(), "index.json"),
	}
	first, err := dropserver.NewWithOptions(options)
	if err != nil {
		t.Fatalf("start first persistent-port server: %v", err)
	}
	firstScan := first.Scan()
	if len(firstScan.Apps) != 1 || firstScan.Apps[0].Port == 0 {
		_ = first.Close()
		t.Fatalf("first assigned port = %#v", firstScan.Apps)
	}
	firstPort := firstScan.Apps[0].Port
	if err := first.Close(); err != nil {
		t.Fatalf("close first persistent-port server: %v", err)
	}

	second, err := dropserver.NewWithOptions(options)
	if err != nil {
		t.Fatalf("start second persistent-port server: %v", err)
	}
	defer func() {
		if closeErr := second.Close(); closeErr != nil {
			t.Errorf("close second persistent-port server: %v", closeErr)
		}
	}()
	secondScan := second.Scan()
	if len(secondScan.Apps) != 1 || secondScan.Apps[0].Port != firstPort {
		t.Fatalf("port after restart = %#v, want %d", secondScan.Apps, firstPort)
	}
	status, body := requestCommandApp(
		t,
		&http.Client{},
		"http://127.0.0.1:"+strconv.Itoa(firstPort)+"/",
	)
	if status != http.StatusOK || body != "Dropserve Node fixture" {
		t.Fatalf("persisted own-port response = %d %q", status, body)
	}
}
