package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestDashboardAtRoot(t *testing.T) {
	t.Parallel()

	server, err := dropserver.New(scanner.Options{Roots: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200; body=%q", result.StatusCode, body)
	}
	if contentType := result.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	for _, expected := range []string{"<title>Dropserve</title>", `id="app-search"`, `autofocus`} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("dashboard body does not contain %q: %s", expected, body)
		}
	}
}
