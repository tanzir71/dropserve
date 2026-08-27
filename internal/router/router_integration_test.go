package router_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/router"
	"github.com/tanzir71/dropserve/internal/scanner"
	staticserver "github.com/tanzir71/dropserve/internal/static"
)

func TestStaticFixtureMounted(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate the fixture root")
	}
	fixturesRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "testdata", "fixtures"))

	result, err := scanner.Scan(scanner.Options{Roots: []string{fixturesRoot}})
	if err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}

	mounts := make([]router.Mount, 0, len(result.Apps))
	for _, application := range result.Apps {
		mounts = append(mounts, router.Mount{
			App:     application,
			Handler: staticserver.New(application),
		})
	}
	handler := router.New(mounts)

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test/static/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	resultResponse := response.Result()
	defer func() {
		_ = resultResponse.Body.Close()
	}()
	body, err := io.ReadAll(resultResponse.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resultResponse.StatusCode != http.StatusOK {
		t.Fatalf("GET /static/ returned %d, want 200; body: %s", resultResponse.StatusCode, body)
	}
	if got := resultResponse.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if got := string(body); !strings.Contains(got, "<h1>Static fixture</h1>") {
		t.Fatalf("body does not contain fixture heading: %s", got)
	}
}
