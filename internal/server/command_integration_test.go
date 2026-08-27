package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestNodeFixtureIsDetectedStartedHealthyAndProxied(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the package.json start rule requires it")
	}
	fixtures, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures"))
	if err != nil {
		t.Fatalf("resolve fixtures: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Roots: []string{fixtures}})
	if err != nil {
		t.Fatalf("create command-app server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close command-app server: %v", closeErr)
		}
	}()

	var detected bool
	for _, application := range server.Scan().Apps {
		if application.Slug == "node" {
			detected = true
			if application.Kind != "command" {
				t.Fatalf("node fixture kind = %q, want command", application.Kind)
			}
		}
	}
	if !detected {
		t.Fatal("node fixture was not discovered")
	}

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/node/",
		nil,
	)
	if err != nil {
		t.Fatalf("create node request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("request node fixture: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read node fixture response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "Dropserve Node fixture" {
		t.Fatalf("node response = %d %q", response.StatusCode, body)
	}

	detailRequest, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/_dropserve/api/apps/node",
		nil,
	)
	if err != nil {
		t.Fatalf("create node detail request: %v", err)
	}
	detailResponse, err := httpServer.Client().Do(detailRequest)
	if err != nil {
		t.Fatalf("request node detail: %v", err)
	}
	defer func() {
		_ = detailResponse.Body.Close()
	}()
	var detail struct {
		Detection string `json:"detection"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
		t.Fatalf("decode node detail: %v", err)
	}
	if detail.Detection != "Node app from package.json start script" {
		t.Fatalf("node detection reason = %q", detail.Detection)
	}
}

func TestPythonFixtureIsDetectedStartedHealthyAndProxied(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python is not installed; CI installs Python so this acceptance test runs there")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "python"))
	if err != nil {
		t.Fatalf("resolve Python fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create Python app server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close Python app server: %v", closeErr)
		}
	}()
	current := server.Scan()
	if len(current.Apps) != 1 || current.Apps[0].Slug != "python" || current.Apps[0].Kind != "command" {
		t.Fatalf("Python detection = %#v", current.Apps)
	}

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	status, body := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/python/")
	if status != http.StatusOK || body != "Dropserve Python fixture" {
		t.Fatalf("Python response = %d %q", status, body)
	}
	detection := requestDetectionReason(t, httpServer.Client(), httpServer.URL+"/_dropserve/api/apps/python")
	if detection != "Python app from server.py" {
		t.Fatalf("Python detection reason = %q", detection)
	}
}

func requestCommandApp(t *testing.T, client *http.Client, target string) (int, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("create command-app request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request command app: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read command-app response: %v", err)
	}
	return response.StatusCode, string(body)
}

func requestDetectionReason(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("create app-detail request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request app detail: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	var detail struct {
		Detection string `json:"detection"`
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatalf("decode app detail: %v", err)
	}
	return detail.Detection
}
