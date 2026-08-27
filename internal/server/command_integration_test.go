package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
	"github.com/tanzir71/dropserve/internal/supervisor"
)

func TestNodeFixtureIsDetectedStartedHealthyAndProxied(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the package.json start rule requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "node"))
	if err != nil {
		t.Fatalf("resolve fixtures: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
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

func TestImmediateFailureRestartsFiveTimesThenCrashes(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the broken package fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "broken"))
	if err != nil {
		t.Fatalf("resolve broken fixture: %v", err)
	}
	server, err := dropserver.NewWithOptions(dropserver.Options{
		Scanner: scanner.Options{Registered: []string{fixture}},
		Supervisor: supervisor.Options{
			RestartDelays: []time.Duration{10 * time.Millisecond},
		},
	})
	if err != nil {
		t.Fatalf("server aborted instead of isolating crashed app: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close crashed-app server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/_dropserve/api/logs/broken",
		nil,
	)
	if err != nil {
		t.Fatalf("create crashed-app log request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("request crashed-app logs: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	var snapshot struct {
		Status   string `json:"status"`
		Attempts int    `json:"attempts"`
		Logs     string `json:"logs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode crashed-app logs: %v", err)
	}
	if snapshot.Status != "crashed" || snapshot.Attempts != 5 {
		t.Fatalf("crashed state = %#v", snapshot)
	}
	if !strings.Contains(snapshot.Logs, "intentional fixture failure") {
		t.Fatalf("crashed logs do not contain fixture error: %q", snapshot.Logs)
	}

	detailRequest, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/_dropserve/api/apps/broken",
		nil,
	)
	if err != nil {
		t.Fatalf("create crashed-app detail request: %v", err)
	}
	detailResponse, err := httpServer.Client().Do(detailRequest)
	if err != nil {
		t.Fatalf("request crashed-app detail: %v", err)
	}
	defer func() {
		_ = detailResponse.Body.Close()
	}()
	var detail struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
		t.Fatalf("decode crashed-app detail: %v", err)
	}
	if detail.Status != "crashed" {
		t.Fatalf("crashed-app detail status = %q, want crashed", detail.Status)
	}
}

func TestCrashedAppDoesNotBlockHealthyApp(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the command fixtures require it")
	}
	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures"))
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	server, err := dropserver.NewWithOptions(dropserver.Options{
		Scanner: scanner.Options{Registered: []string{
			filepath.Join(fixtureRoot, "broken"),
			filepath.Join(fixtureRoot, "node"),
		}},
		Supervisor: supervisor.Options{
			RestartDelays: []time.Duration{10 * time.Millisecond},
		},
	})
	if err != nil {
		t.Fatalf("create mixed-health server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close mixed-health server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	dashboardStatus, dashboardBody := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/")
	if dashboardStatus != http.StatusOK || !strings.Contains(dashboardBody, "Dropserve") {
		t.Fatalf("dashboard with crashed app = %d %q", dashboardStatus, dashboardBody)
	}
	healthyStatus, healthyBody := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/node/")
	if healthyStatus != http.StatusOK || healthyBody != "Dropserve Node fixture" {
		t.Fatalf("healthy app beside crashed app = %d %q", healthyStatus, healthyBody)
	}
}

func TestShutdownLeavesNoCommandChild(t *testing.T) {
	t.Parallel()

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
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create shutdown-test server: %v", err)
	}
	serverClosed := false
	defer func() {
		if !serverClosed {
			_ = server.Close()
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	status, body := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/node/pid")
	if status != http.StatusOK {
		t.Fatalf("PID endpoint status = %d, want 200", status)
	}
	var childPID uint32
	if _, err := fmt.Sscanf(strings.TrimSpace(body), "%d", &childPID); err != nil {
		t.Fatalf("parse child PID from %q: %v", body, err)
	}
	alive, err := processAlive(childPID)
	if err != nil {
		t.Fatalf("inspect child PID %d before shutdown: %v", childPID, err)
	}
	if !alive {
		t.Fatalf("child PID %d was not alive before shutdown", childPID)
	}

	if err := server.Close(); err != nil {
		t.Fatalf("close shutdown-test server: %v", err)
	}
	serverClosed = true
	deadline := time.Now().Add(5 * time.Second)
	for {
		alive, err = processAlive(childPID)
		if err != nil {
			t.Fatalf("inspect child PID %d after shutdown: %v", childPID, err)
		}
		if !alive {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child PID %d survived Dropserve shutdown for five seconds", childPID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMissingRuntimeMountsFriendlyNeedsRuntimePage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "node"))
	if err != nil {
		t.Fatalf("resolve Node fixture: %v", err)
	}
	server, err := dropserver.NewWithOptions(dropserver.Options{
		Scanner: scanner.Options{Registered: []string{fixture}},
		Supervisor: supervisor.Options{
			RestartDelays: []time.Duration{10 * time.Millisecond},
		},
	})
	if err != nil {
		t.Fatalf("mount app with missing runtime: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close missing-runtime server: %v", closeErr)
		}
	}()
	current := server.Scan()
	if len(current.Apps) != 1 || current.Apps[0].Status != "needs-runtime" {
		t.Fatalf("missing-runtime scan = %#v", current.Apps)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	status, body := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/node/")
	if status != http.StatusOK {
		t.Fatalf("missing-runtime page status = %d, want 200; body=%q", status, body)
	}
	if !strings.Contains(body, "Node.js") || !strings.Contains(body, "install") {
		t.Fatalf("missing-runtime page does not explain the fix: %q", body)
	}
}

func TestAutostartFalseStartsOnFirstRequest(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the lazy Node fixture requires it")
	}
	appRoot := filepath.Join(t.TempDir(), "lazy-node")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create lazy app: %v", err)
	}
	files := map[string]string{
		"package.json":   `{"name":"lazy-node","private":true,"scripts":{"start":"node server.js"}}`,
		"dropserve.json": `{"autostart":false}`,
		"server.js": `const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
fs.writeFileSync(path.join(__dirname, "started"), String(process.pid));
http.createServer((_request, response) => response.end("lazy fixture ready"))
  .listen(Number(process.env.PORT), process.env.HOST);`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(appRoot, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write lazy fixture %s: %v", name, err)
		}
	}
	marker := filepath.Join(appRoot, "started")
	server, err := dropserver.New(scanner.Options{Registered: []string{appRoot}})
	if err != nil {
		t.Fatalf("create lazy-start server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close lazy-start server: %v", closeErr)
		}
	}()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("lazy app started during scan; marker error = %v", err)
	}
	current := server.Scan()
	if len(current.Apps) != 1 || current.Apps[0].Autostart || current.Apps[0].Status != "stopped" {
		t.Fatalf("lazy app scan = %#v", current.Apps)
	}

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	status, body := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/lazy-node/")
	if status != http.StatusOK || body != "lazy fixture ready" {
		t.Fatalf("lazy first response = %d %q", status, body)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("lazy app did not start on first request: %v", err)
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
