package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/doctor"
	"github.com/tanzir71/dropserve/internal/firstrun"
	"github.com/tanzir71/dropserve/internal/router"
	"github.com/tanzir71/dropserve/internal/scanner"
	"github.com/tanzir71/dropserve/internal/state"
	staticserver "github.com/tanzir71/dropserve/internal/static"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(content []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(content)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

func TestCompletedFirstRunSuppliesEditedAppsRootToDesktopMode(t *testing.T) {
	defaultRoot := filepath.Join("default", "Apps")
	editedRoot := filepath.Join("chosen", "Apps")

	if got := desktopAppsRoot(defaultRoot, firstrun.Result{Shown: true, AppsRoot: editedRoot}); got != editedRoot {
		t.Fatalf("desktopAppsRoot() = %q, want edited first-run root %q", got, editedRoot)
	}
	if got := desktopAppsRoot(defaultRoot, firstrun.Result{}); got != defaultRoot {
		t.Fatalf("desktopAppsRoot() = %q, want existing configured root %q", got, defaultRoot)
	}
}

func TestCommandSignalsIncludeInterruptAndTerminate(t *testing.T) {
	signals := commandSignals()
	wants := []os.Signal{os.Interrupt, syscall.SIGTERM}
	for _, want := range wants {
		found := false
		for _, got := range signals {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("commandSignals() = %v, want %v", signals, want)
		}
	}
}

func TestLoopbackListenerNeverPublishesLANAddress(t *testing.T) {
	if !listenerExcludesLAN(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8000}) {
		t.Fatal("loopback-only listener was treated as reachable from the LAN")
	}
	if listenerExcludesLAN(&net.TCPAddr{IP: net.IPv4zero, Port: 8000}) {
		t.Fatal("wildcard listener was treated as loopback-only")
	}
}

func TestHTTPSListenerFailureDegradesToHTTPOnly(t *testing.T) {
	sandbox := t.TempDir()
	appsRoot := filepath.Join(sandbox, "Apps")
	if err := os.MkdirAll(appsRoot, 0o750); err != nil {
		t.Fatalf("create Apps root: %v", err)
	}
	occupied, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy HTTPS port: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	_, portText, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatalf("read occupied HTTPS port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse occupied HTTPS port: %v", err)
	}
	configuration := config.Default()
	configuration.Server.AppsRoots = []string{appsRoot}
	configuration.Server.Bind = "127.0.0.1"
	configuration.Server.HTTPSPort = port
	configPath := filepath.Join(sandbox, "config.toml")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout lockedBuffer
	var stderr lockedBuffer
	ready := make(chan string, 1)
	done := make(chan int, 1)
	go func() {
		done <- serveCommandContextWithReady(
			ctx,
			[]string{"--config", configPath, "--state", filepath.Join(sandbox, "state.json"), "--listen", "127.0.0.1:0"},
			&stdout,
			&stderr,
			"",
			func(address string) { ready <- address },
			nil,
			nil,
		)
	}()

	var address string
	select {
	case address = <-ready:
	case code := <-done:
		t.Fatalf("server exited with %d before HTTP became ready; stderr=%q", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("HTTP server did not become ready; stderr=%q", stderr.String())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/", nil)
	if err != nil {
		t.Fatalf("create HTTP request: %v", err)
	}
	response, err := http.DefaultClient.Do(request) // #nosec G107 -- address is the loopback listener returned above.
	if err != nil {
		t.Fatalf("HTTP stopped because HTTPS failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HTTP after HTTPS failure = %d, want 200", response.StatusCode)
	}
	if !strings.Contains(stderr.String(), "HTTPS") || !strings.Contains(stderr.String(), portText) {
		t.Fatalf("HTTPS failure warning = %q, want HTTPS and occupied port %s", stderr.String(), portText)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("server exit after HTTP-only degradation = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
}

func TestServeWithNoPacksKeepsBaseBinaryOperational(t *testing.T) {
	sandbox := t.TempDir()
	appsRoot := filepath.Join(sandbox, "Apps")
	staticRoot := filepath.Join(appsRoot, "static")
	phpRoot := filepath.Join(appsRoot, "php")
	if err := os.MkdirAll(staticRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(phpRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticRoot, "index.html"), []byte("pack-free static"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(phpRoot, "index.php"), []byte("<?php echo 'optional';"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration := config.Default()
	configuration.Server.AppsRoots = []string{appsRoot}
	configuration.Server.Bind = "127.0.0.1"
	configPath := filepath.Join(sandbox, "config.toml")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(sandbox, "machine", "state.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout lockedBuffer
	var stderr lockedBuffer
	ready := make(chan string, 1)
	done := make(chan int, 1)
	go func() {
		done <- serveCommandContextWithReady(
			ctx,
			[]string{"--config", configPath, "--state", statePath, "--listen", "127.0.0.1:0"},
			&stdout,
			&stderr,
			"",
			func(address string) { ready <- address },
			nil,
			nil,
		)
	}()
	var address string
	select {
	case address = <-ready:
	case code := <-done:
		t.Fatalf("pack-free server exited with %d; stderr=%q", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("pack-free server did not become ready; stderr=%q", stderr.String())
	}
	assertBody := func(path string, marker string) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request) // #nosec G107 -- address is the test-owned loopback listener.
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), marker) {
			t.Fatalf("GET %s = %d %q, want 200 containing %q", path, response.StatusCode, body, marker)
		}
	}
	assertBody("/static/", "pack-free static")
	assertBody("/php/", "Install the optional PHP pack")
	addOnsRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/_dropserve/api/addons", nil)
	if err != nil {
		t.Fatal(err)
	}
	addOnsResponse, err := http.DefaultClient.Do(addOnsRequest) // #nosec G107 -- address is the test-owned loopback listener.
	if err != nil {
		t.Fatal(err)
	}
	addOnsBody, readErr := io.ReadAll(addOnsResponse.Body)
	_ = addOnsResponse.Body.Close()
	if readErr != nil || addOnsResponse.StatusCode != http.StatusOK || strings.Contains(string(addOnsBody), `"installed":true`) {
		t.Fatalf("pack-free add-ons = %d %q read=%v", addOnsResponse.StatusCode, addOnsBody, readErr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(statePath), "runtimes")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pack-free startup created a runtime directory: %v", err)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("pack-free server exit = %d; stderr=%q", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pack-free server did not stop")
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(version) returned %d; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); !strings.HasPrefix(got, "dropserve ") || !strings.Contains(got, "(") {
		t.Fatalf("version output %q does not contain the product, version, and commit", got)
	}
}

func TestHealthzCommandChecksTheRunningServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_dropserve/healthz" {
			http.NotFound(response, request)
			return
		}
		_, _ = io.WriteString(response, "ok")
	}))
	address := strings.TrimPrefix(server.URL, "http://")
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := state.Save(statePath, state.State{HTTPPort: port}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := healthzCommand([]string{"--state", statePath}, &stdout, &stderr); code != 0 || stdout.String() != "ok\n" {
		t.Fatalf("live healthz = %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	server.Close()
	stdout.Reset()
	stderr.Reset()
	if code := healthzCommand([]string{"--state", statePath}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "not healthy") {
		t.Fatalf("stopped healthz = %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestUnknownCommandNamesTheFix(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(nope) returned %d, want 2", code)
	}
	if got := stderr.String(); !strings.Contains(got, "dropserve help") {
		t.Fatalf("error %q does not name the recovery command", got)
	}
}

func TestDoctorReportsSyncRootWarning(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "Dropbox", "Apps")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("create sync-backed root: %v", err)
	}
	configuration := config.Default()
	configuration.Server.AppsRoots = []string{root}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save doctor config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runWithConfigPath([]string{"doctor"}, &stdout, &stderr, configPath); code != 0 {
		t.Fatalf("doctor returned %d; stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, root) || !strings.Contains(output, `%USERPROFILE%\Dropserve`) {
		t.Fatalf("doctor output = %q, want root and recommended location", output)
	}
}

func TestDoctorExitCodesAndCoversSupportSurface(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	appsRoot := filepath.Join(sandbox, "Apps")
	appRoot := filepath.Join(appsRoot, "sample")
	if err := os.MkdirAll(appRoot, 0o750); err != nil {
		t.Fatalf("create sample app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte("<h1>Sample</h1>"), 0o600); err != nil {
		t.Fatalf("write sample app: %v", err)
	}
	configuration := config.Default()
	configuration.Server.AppsRoots = []string{appsRoot}
	configPath := filepath.Join(sandbox, "config.toml")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save doctor config: %v", err)
	}
	statePath := filepath.Join(sandbox, "state.json")
	if err := state.Save(statePath, state.State{HTTPPort: 8000}); err != nil {
		t.Fatalf("save doctor state: %v", err)
	}
	probes := doctor.Probes{
		OS: "linux",
		LookPath: func(name string) (string, error) {
			return "/usr/bin/" + name, nil
		},
		RunCommand: func(string, ...string) ([]byte, error) {
			return []byte("healthy"), nil
		},
		ProbeMDNS:        func() error { return nil },
		AutostartEnabled: func() (bool, error) { return true, nil },
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	arguments := []string{"--config", configPath, "--state", statePath}
	if code := doctorCommandWithProbes(arguments, &stdout, &stderr, "", probes); code != 0 {
		t.Fatalf("healthy doctor returned %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	for _, label := range []string{
		"Version:", "HTTP port:", "Windows excluded TCP port ranges:", "Windows firewall rule:",
		"Apps folder:", "App sample:", "App warnings:", "Runtime node:", "Runtime python:",
		"Runtime php:", "mDNS bind:", "Tailscale:", "Autostart:", "Error logs:",
	} {
		if !strings.Contains(stdout.String(), label) {
			t.Errorf("doctor output does not contain %q:\n%s", label, stdout.String())
		}
	}

	configuration.Server.AppsRoots = []string{filepath.Join(sandbox, "missing-root")}
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save failing doctor config: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := doctorCommandWithProbes(arguments, &stdout, &stderr, "", probes); code != 1 {
		t.Fatalf("doctor with missing required root returned %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
}

func TestAddRegistersPathWithoutChangingApp(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	appPath := filepath.Join(sandbox, "external", "invoice-tool")
	if err := os.MkdirAll(appPath, 0o750); err != nil {
		t.Fatalf("create external app: %v", err)
	}
	index := []byte("<!doctype html><title>Invoice Tool</title><h1>Invoice Tool</h1>")
	if err := os.WriteFile(filepath.Join(appPath, "index.html"), index, 0o600); err != nil {
		t.Fatalf("write external app: %v", err)
	}
	configDir := filepath.Join(sandbox, "dropserve-state")
	if err := os.Mkdir(configDir, 0o750); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	before := snapshotTestTree(t, appPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runWithConfigPath([]string{"add", appPath}, &stdout, &stderr, configPath); code != 0 {
		t.Fatalf("add returned %d; stderr: %s", code, stderr.String())
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if len(loaded.Server.RegisteredApps) != 1 {
		t.Fatalf("registered app count = %d, want 1", len(loaded.Server.RegisteredApps))
	}
	absoluteAppPath, err := filepath.Abs(appPath)
	if err != nil {
		t.Fatalf("resolve app path: %v", err)
	}
	if got := loaded.Server.RegisteredApps[0]; got != absoluteAppPath {
		t.Fatalf("registered path = %q, want %q", got, absoluteAppPath)
	}
	if after := snapshotTestTree(t, appPath); !reflect.DeepEqual(before, after) {
		t.Fatalf("app tree changed during add\nbefore: %#v\nafter:  %#v", before, after)
	}
	if info, err := os.Lstat(appPath); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("app path became or was replaced by a symlink: info=%v err=%v", info, err)
	}
	stateEntries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("read config directory: %v", err)
	}
	if len(stateEntries) != 1 || stateEntries[0].Name() != "config.toml" {
		t.Fatalf("add left files besides config.toml: %v", stateEntries)
	}

	scan, err := scanner.Scan(scanner.Options{Registered: loaded.Server.RegisteredApps})
	if err != nil {
		t.Fatalf("scan registered app: %v", err)
	}
	if len(scan.Apps) != 1 {
		t.Fatalf("registered scan returned %d apps, want 1", len(scan.Apps))
	}
	application := scan.Apps[0]
	handler := router.New([]router.Mount{{App: application, Handler: staticserver.New(application)}})
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/invoice-tool/",
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read registered app: %v", err)
	}
	if result.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Invoice Tool")) {
		t.Fatalf("registered app response = %d %q, want 200 with Invoice Tool", result.StatusCode, body)
	}
}

func TestPersistedPortIsPreferredOnRestart(t *testing.T) {
	t.Parallel()

	var listenConfig net.ListenConfig
	probe, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe available port: %v", err)
	}
	_, portText, err := net.SplitHostPort(probe.Addr().String())
	if err != nil {
		_ = probe.Close()
		t.Fatalf("read probed port: %v", err)
	}
	preferredPort, err := strconv.Atoi(portText)
	if err != nil {
		_ = probe.Close()
		t.Fatalf("parse probed port: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatalf("release probed port: %v", err)
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	originalWarning := state.Warning{
		Code:    "port_fallback",
		Message: "Dropserve selected this fallback on the first start.",
	}
	if err := state.Save(statePath, state.State{
		HTTPPort: preferredPort,
		Warnings: []state.Warning{originalWarning},
	}); err != nil {
		t.Fatalf("save prior state: %v", err)
	}
	configuration := config.Default()
	configuration.Server.HTTPPort = 0
	listener, err := acquireMainListener(
		context.Background(),
		"",
		"127.0.0.1",
		statePath,
		configuration,
	)
	if err != nil {
		t.Fatalf("acquire persisted port: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()
	_, selectedPortText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("read selected port: %v", err)
	}
	selectedPort, err := strconv.Atoi(selectedPortText)
	if err != nil {
		t.Fatalf("parse selected port: %v", err)
	}
	if selectedPort != preferredPort {
		t.Fatalf("selected port = %d, want persisted %d", selectedPort, preferredPort)
	}

	persisted, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("load updated state: %v", err)
	}
	if !reflect.DeepEqual(persisted.Warnings, []state.Warning{originalWarning}) {
		t.Fatalf("warnings = %#v, want original warning %#v", persisted.Warnings, originalWarning)
	}
}

type testTreeEntry struct {
	Mode             fs.FileMode
	Size             int64
	ModifiedUnixNano int64
	Hash             [sha256.Size]byte
}

func snapshotTestTree(t *testing.T, root string) map[string]testTreeEntry {
	t.Helper()

	snapshot := make(map[string]testTreeEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := testTreeEntry{
			Mode:             info.Mode(),
			Size:             info.Size(),
			ModifiedUnixNano: info.ModTime().UnixNano(),
		}
		if !entry.IsDir() {
			// #nosec G304,G122 -- path is produced by WalkDir below the test's private app root.
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item.Hash = sha256.Sum256(data)
		}
		snapshot[relative] = item
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}
