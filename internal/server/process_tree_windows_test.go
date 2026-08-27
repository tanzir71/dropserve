//go:build windows

package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestProcessTreeIsKilled(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the process-tree fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "process-tree"))
	if err != nil {
		t.Fatalf("resolve process-tree fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create process-tree server: %v", err)
	}
	serverClosed := false
	defer func() {
		if !serverClosed {
			_ = server.Close()
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/process-tree/",
		nil,
	)
	if err != nil {
		t.Fatalf("create process-tree request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("request process-tree fixture: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read grandchild PID: %v", readErr)
	}
	var grandchildPID uint32
	if _, err := fmt.Sscanf(strings.TrimSpace(string(body)), "%d", &grandchildPID); err != nil {
		t.Fatalf("parse grandchild PID from %q: %v", body, err)
	}
	alive, err := processAlive(grandchildPID)
	if err != nil {
		t.Fatalf("inspect grandchild PID %d before shutdown: %v", grandchildPID, err)
	}
	if !alive {
		t.Fatalf("grandchild PID %d was not alive before shutdown", grandchildPID)
	}

	if err := server.Close(); err != nil {
		t.Fatalf("close process-tree server: %v", err)
	}
	serverClosed = true
	deadline := time.Now().Add(5 * time.Second)
	for {
		alive, err = processAlive(grandchildPID)
		if err != nil {
			t.Fatalf("inspect grandchild PID %d after shutdown: %v", grandchildPID, err)
		}
		if !alive {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild PID %d survived Dropserve shutdown for five seconds", grandchildPID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
