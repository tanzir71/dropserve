package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestFolderAddedIsServedWithinTwoSeconds(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create live server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close live server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	appRoot := filepath.Join(root, "live-notes")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte("<h1>Live notes</h1>"), 0o600); err != nil {
		t.Fatalf("write app index: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		request, requestErr := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			httpServer.URL+"/live-notes/",
			nil,
		)
		if requestErr != nil {
			t.Fatalf("create app request: %v", requestErr)
		}
		response, responseErr := httpServer.Client().Do(request)
		if responseErr != nil {
			t.Fatalf("request live app: %v", responseErr)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			t.Fatalf("read live app: %v", readErr)
		}
		if response.StatusCode == http.StatusOK {
			if string(body) != "<h1>Live notes</h1>" {
				t.Fatalf("live app body = %q", body)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("live app returned %d after two-second deadline; body=%q", response.StatusCode, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
