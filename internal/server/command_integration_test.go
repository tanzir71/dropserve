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
