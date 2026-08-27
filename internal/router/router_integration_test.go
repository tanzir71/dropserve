package router_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

	handler := fixtureRouter(t)
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

func TestMissingTrailingSlashRedirects(t *testing.T) {
	t.Parallel()

	handler := fixtureRouter(t)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/static?from=dashboard",
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /static returned %d, want 301", response.Code)
	}
	if got, want := response.Header().Get("Location"), "/static/?from=dashboard"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestSlugCollisionsRemainReachable(t *testing.T) {
	t.Parallel()

	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeStaticApp(t, firstRoot, "notes", "first root")
	writeStaticApp(t, secondRoot, "notes", "second root")

	result, err := scanner.Scan(scanner.Options{Roots: []string{firstRoot, secondRoot}})
	if err != nil {
		t.Fatalf("scan colliding roots: %v", err)
	}
	if len(result.Apps) != 2 {
		t.Fatalf("scan returned %d apps, want 2", len(result.Apps))
	}
	if got, want := result.Apps[0].Slug, "notes"; got != want {
		t.Fatalf("first slug = %q, want %q", got, want)
	}
	if got, want := result.Apps[1].Slug, "notes-2"; got != want {
		t.Fatalf("second slug = %q, want %q", got, want)
	}

	foundCollisionWarning := false
	for _, warning := range result.Warnings {
		if warning.Code == "slug_collision" &&
			strings.Contains(warning.Message, filepath.Join(firstRoot, "notes")) &&
			strings.Contains(warning.Message, filepath.Join(secondRoot, "notes")) {
			foundCollisionWarning = true
		}
	}
	if !foundCollisionWarning {
		t.Fatal("collision warning did not name both app paths")
	}

	mounts := make([]router.Mount, 0, len(result.Apps))
	for _, application := range result.Apps {
		mounts = append(mounts, router.Mount{App: application, Handler: staticserver.New(application)})
	}
	handler := router.New(mounts)
	assertBodyContains(t, handler, "/notes/", "first root")
	assertBodyContains(t, handler, "/notes-2/", "second root")
}

func fixtureRouter(t *testing.T) http.Handler {
	t.Helper()

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
	return router.New(mounts)
}

func writeStaticApp(t *testing.T, root, name, body string) {
	t.Helper()

	appRoot := filepath.Join(root, name)
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create app root: %v", err)
	}
	document := "<!doctype html><title>" + body + "</title><p>" + body + "</p>"
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(document), 0o600); err != nil {
		t.Fatalf("write app index: %v", err)
	}
}

func assertBodyContains(t *testing.T, handler http.Handler, requestPath, want string) {
	t.Helper()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test"+requestPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	result := response.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", requestPath, err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %d, want 200; body: %s", requestPath, result.StatusCode, body)
	}
	if !strings.Contains(string(body), want) {
		t.Fatalf("GET %s body %q does not contain %q", requestPath, body, want)
	}
}
