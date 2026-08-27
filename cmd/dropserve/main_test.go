package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"syscall"
	"testing"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/router"
	"github.com/tanzir71/dropserve/internal/scanner"
	"github.com/tanzir71/dropserve/internal/state"
	staticserver "github.com/tanzir71/dropserve/internal/static"
)

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
