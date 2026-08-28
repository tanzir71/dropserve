package php

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPoolRestartsKilledWorkerWithinFiveSeconds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := StartPool(ctx, PoolOptions{
		Workers: 2,
		Command: func(commandContext context.Context, address string) *exec.Cmd {
			command := exec.CommandContext(commandContext, os.Args[0], "-test.run=TestPHPPoolWorkerProcess", "--", "-b", address) // #nosec G204,G702 -- the test executable and loopback address are controlled here.
			command.Env = append(os.Environ(), "DROPSERVE_TEST_PHP_POOL_WORKER=1")
			return command
		},
		StartTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("start PHP pool: %v", err)
	}
	defer func() {
		if closeErr := pool.Close(); closeErr != nil {
			t.Errorf("close PHP pool: %v", closeErr)
		}
	}()

	pool.mu.Lock()
	original := pool.workers[0]
	originalPID := original.command.Process.Pid
	pool.mu.Unlock()
	if err := original.command.Process.Kill(); err != nil {
		t.Fatalf("kill PHP worker %d: %v", originalPID, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	recovered := false
	for time.Now().Before(deadline) {
		pool.mu.Lock()
		replacement := pool.workers[0]
		replaced := replacement != original && replacement.command.Process.Pid != originalPID
		pool.mu.Unlock()
		if replaced && acceptsConnections(replacement.address) {
			recovered = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !recovered {
		t.Fatalf("PHP worker %d was not replaced within five seconds", originalPID)
	}
	handler := pool.Handler(t.TempDir(), "php")
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/php/", nil)
	request.URL.Path = "/"
	request.RequestURI = "/php/"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "worker ready" {
		t.Fatalf("recovered PHP pool response = %d %q", response.Code, response.Body.String())
	}
}

func acceptsConnections(address string) bool {
	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	connection, err := dialer.DialContext(context.Background(), "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func TestPHPPoolWorkerProcess(t *testing.T) {
	if os.Getenv("DROPSERVE_TEST_PHP_POOL_WORKER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) <= separator+2 || os.Args[separator+1] != "-b" {
		t.Fatalf("PHP worker arguments = %q", os.Args)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", os.Args[separator+2])
	if err != nil {
		t.Fatalf("listen for FastCGI pool worker: %v", err)
	}
	if err := fcgi.Serve(listener, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(response, "worker ready")
	})); err != nil {
		t.Fatalf("serve FastCGI pool worker: %v", err)
	}
}
