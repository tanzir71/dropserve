package runtimes

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type fakePHPRuntime struct {
	closed bool
}

type fakeDatabaseRuntime struct {
	closed     bool
	connection string
}

func (runtime *fakeDatabaseRuntime) Running() bool      { return !runtime.closed }
func (runtime *fakeDatabaseRuntime) Connection() string { return runtime.connection }
func (runtime *fakeDatabaseRuntime) Close() error {
	runtime.closed = true
	return nil
}

func (runtime *fakePHPRuntime) Handler(_, _ string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "php ready")
	})
}

func (runtime *fakePHPRuntime) Close() error {
	runtime.closed = true
	return nil
}

func TestManagerInstallsStartsAndRemovesPHPWithoutAppWrites(t *testing.T) {
	t.Parallel()
	payload := runtimeZIP(t, "php-cgi.exe", "verified php")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(payload)
	}))
	t.Cleanup(server.Close)
	hash := sha256.Sum256(payload)
	pack := Pack{
		Name: "php", Version: "test", OS: runtime.GOOS, Arch: runtime.GOARCH,
		URL: server.URL, SHA256: fmt.Sprintf("%x", hash), Format: FormatZIP, Executable: "php-cgi.exe",
	}
	state := t.TempDir()
	appRoot := t.TempDir()
	appFile := filepath.Join(appRoot, "index.php")
	if err := os.WriteFile(appFile, []byte("app bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := 0
	worker := &fakePHPRuntime{}
	manager, err := NewManager(ManagerOptions{
		Context: context.Background(), StateDirectory: state, Packs: []Pack{pack},
		PHPStarter: func(_ context.Context, executable, iniPath string, _ io.Writer) (PHPRuntime, error) {
			started++
			if _, err := os.Stat(executable); err != nil {
				t.Fatalf("PHP executable was not installed: %v", err)
			}
			if _, err := os.Stat(iniPath); err != nil {
				t.Fatalf("PHP INI was not generated: %v", err)
			}
			return worker, nil
		},
	})
	if err != nil {
		t.Fatalf("create add-on manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	if status := manager.Statuses()[0]; status.Installed || status.Running {
		t.Fatalf("fresh PHP status = %#v", status)
	}
	if err := manager.Change(context.Background(), "php", "install"); err != nil {
		t.Fatalf("install PHP: %v", err)
	}
	if status := manager.Statuses()[0]; !status.Installed || !status.Running || started != 1 {
		t.Fatalf("installed PHP status = %#v starts=%d", status, started)
	}
	handler, err := manager.PHPHandler(appRoot, "php")
	if err != nil || handler == nil {
		t.Fatalf("PHP handler = %v, %v", handler, err)
	}
	if err := manager.Change(context.Background(), "php", "remove"); err != nil {
		t.Fatalf("remove PHP: %v", err)
	}
	if !worker.closed {
		t.Error("removing PHP did not stop its worker pool")
	}
	content, err := os.ReadFile(appFile) // #nosec G304 -- appFile is inside this test's temporary app root.
	if err != nil || string(content) != "app bytes" {
		t.Fatalf("app changed while removing PHP: %q, %v", content, err)
	}
	if status := manager.Statuses()[0]; status.Installed || status.Running {
		t.Fatalf("removed PHP status = %#v", status)
	}
	if _, err := os.Stat(filepath.Join(state, "php")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PHP managed settings remain after removal: %v", err)
	}
}

func TestManagerWithNoPacksStartsNothing(t *testing.T) {
	t.Parallel()
	starts := 0
	manager, err := NewManager(ManagerOptions{
		Context: context.Background(), StateDirectory: t.TempDir(), Packs: []Pack{},
		PHPStarter: func(context.Context, string, string, io.Writer) (PHPRuntime, error) {
			starts++
			return &fakePHPRuntime{}, nil
		},
	})
	if err != nil {
		t.Fatalf("create pack-free manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if len(manager.Statuses()) != 0 || starts != 0 {
		t.Fatalf("pack-free manager statuses=%v starts=%d", manager.Statuses(), starts)
	}
	if handler, err := manager.PHPHandler(t.TempDir(), "php"); err != nil || handler != nil {
		t.Fatalf("pack-free PHP handler = %v, %v", handler, err)
	}
}

func TestManagerKeepsDatabaseDataUnderStateAndReturnsConnection(t *testing.T) {
	t.Parallel()
	payload := runtimeZIP(t, "bin/mariadbd.exe", "verified database")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(payload)
	}))
	t.Cleanup(server.Close)
	hash := sha256.Sum256(payload)
	pack := Pack{
		Name: "mariadb", Version: "test", OS: runtime.GOOS, Arch: runtime.GOARCH,
		URL: server.URL, SHA256: fmt.Sprintf("%x", hash), Format: FormatZIP, Executable: "bin/mariadbd.exe",
	}
	state := t.TempDir()
	var startedDataDirectory string
	process := &fakeDatabaseRuntime{connection: "mysql://root@127.0.0.1:33061/"}
	manager, err := NewManager(ManagerOptions{
		Context: context.Background(), StateDirectory: state, Packs: []Pack{pack},
		DatabaseStarter: func(_ context.Context, received Pack, executable, dataDirectory string, _ io.Writer) (DatabaseRuntime, error) {
			if received != pack {
				t.Fatalf("database starter pack = %#v", received)
			}
			if _, err := os.Stat(executable); err != nil {
				t.Fatalf("database executable was not installed: %v", err)
			}
			startedDataDirectory = dataDirectory
			if err := os.WriteFile(filepath.Join(dataDirectory, "managed.db"), []byte("database bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			return process, nil
		},
	})
	if err != nil {
		t.Fatalf("create database add-on manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Change(context.Background(), "mariadb", "install"); err != nil {
		t.Fatalf("install MariaDB: %v", err)
	}
	if status := manager.Statuses()[0]; !status.Installed || status.Running {
		t.Fatalf("installed MariaDB status = %#v", status)
	}
	if err := manager.Change(context.Background(), "mariadb", "start"); err != nil {
		t.Fatalf("start MariaDB: %v", err)
	}
	expectedData := filepath.Join(state, "databases", "mariadb", "data")
	if startedDataDirectory != expectedData {
		t.Fatalf("MariaDB data directory = %q, want %q", startedDataDirectory, expectedData)
	}
	if status := manager.Statuses()[0]; !status.Running || status.Connection != process.connection {
		t.Fatalf("running MariaDB status = %#v", status)
	}
	if err := manager.Change(context.Background(), "mariadb", "remove"); err != nil {
		t.Fatalf("remove MariaDB: %v", err)
	}
	if !process.closed {
		t.Error("removing MariaDB did not stop its child process")
	}
	if _, err := os.Stat(filepath.Join(state, "databases", "mariadb")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("MariaDB managed data remains after removal: %v", err)
	}
}

func runtimeZIP(t *testing.T, name, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, content); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
