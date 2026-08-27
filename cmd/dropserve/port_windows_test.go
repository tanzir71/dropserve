//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/state"
)

func TestPortFallbackAndStatus(t *testing.T) {
	port80 := occupyPort(t, 80)
	if port80 != nil {
		defer func() {
			_ = port80.Close()
		}()
	}
	port8080 := occupyPort(t, 8080)
	if port8080 != nil {
		defer func() {
			_ = port8080.Close()
		}()
	}

	root := t.TempDir()
	appRoot := filepath.Join(root, "fixture")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create fixture app: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(appRoot, "index.html"),
		[]byte("<!doctype html><h1>Port fallback fixture</h1>"),
		0o600,
	); err != nil {
		t.Fatalf("write fixture app: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")

	serverContext, cancelServer := context.WithCancel(context.Background())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := make(chan int, 1)
	go func() {
		exitCode <- serveCommandContext(
			serverContext,
			[]string{"--bind", "127.0.0.1", "--root", root, "--state", statePath},
			&stdout,
			&stderr,
			"",
		)
	}()
	defer cancelServer()

	var persisted state.State
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		persisted, err = state.Load(statePath)
		if err == nil && persisted.HTTPPort != 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if persisted.HTTPPort != 8000 {
		cancelServer()
		t.Fatalf("selected port = %d, want 8000; stdout=%s stderr=%s", persisted.HTTPPort, stdout.String(), stderr.String())
	}

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://127.0.0.1:8000/fixture/",
		nil,
	)
	if err != nil {
		t.Fatalf("create fixture request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("fetch fallback server: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read fallback response: read=%v close=%v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Port fallback fixture")) {
		t.Fatalf("fallback response = %d %q", response.StatusCode, body)
	}

	var statusOutput bytes.Buffer
	if code := statusCommand([]string{"--state", statePath}, &statusOutput, &stderr); code != 0 {
		t.Fatalf("status returned %d; stderr=%s", code, stderr.String())
	}
	var status struct {
		Port     int             `json:"port"`
		Warnings []state.Warning `json:"warnings"`
	}
	if err := json.Unmarshal(statusOutput.Bytes(), &status); err != nil {
		t.Fatalf("decode status %q: %v", statusOutput.String(), err)
	}
	if status.Port != 8000 {
		t.Fatalf("status port = %d, want 8000", status.Port)
	}
	foundWarning := false
	for _, warning := range status.Warnings {
		if warning.Code == "port_fallback" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("status warnings do not include port_fallback: %#v", status.Warnings)
	}

	cancelServer()
	select {
	case code := <-exitCode:
		if code != 0 {
			t.Fatalf("serve returned %d; stderr=%s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func occupyPort(t *testing.T, port int) net.Listener {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(
		context.Background(),
		"tcp4",
		fmt.Sprintf("127.0.0.1:%d", port),
	)
	if err != nil {
		// Windows CI images can reserve port 80 through http.sys or an excluded
		// range. The failed real bind proves Dropserve will have to skip it just
		// as surely as a listener owned by this test would.
		t.Logf("port %d was already unavailable: %v", port, err)
		return nil
	}
	return listener
}
