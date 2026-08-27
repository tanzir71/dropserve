package router_test

import (
	"context"
	"crypto/sha256"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

func TestAppFolderIsReadOnlyToUs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeStaticApp(t, root, "immutable", "read only")
	assets := filepath.Join(root, "immutable", "assets")
	if err := os.Mkdir(assets, 0o750); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assets, "data.txt"), []byte("unchanged payload"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	appRoot := filepath.Join(root, "immutable")
	before := snapshotTree(t, appRoot)

	result, err := scanner.Scan(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("scan immutable fixture: %v", err)
	}
	if len(result.Apps) != 1 {
		t.Fatalf("scan returned %d apps, want 1", len(result.Apps))
	}
	application := result.Apps[0]
	handler := router.New([]router.Mount{{App: application, Handler: staticserver.New(application)}})
	assertBodyContains(t, handler, "/immutable/", "read only")
	assertBodyContains(t, handler, "/immutable/assets/data.txt", "unchanged payload")

	after := snapshotTree(t, appRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("app tree changed during scan and serve\nbefore: %#v\nafter:  %#v", before, after)
	}
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

type treeSnapshotEntry struct {
	Mode             fs.FileMode
	Size             int64
	ModifiedUnixNano int64
	Hash             [sha256.Size]byte
}

func snapshotTree(t *testing.T, root string) map[string]treeSnapshotEntry {
	t.Helper()

	snapshot := make(map[string]treeSnapshotEntry)
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
		item := treeSnapshotEntry{
			Mode:             info.Mode(),
			Size:             info.Size(),
			ModifiedUnixNano: info.ModTime().UnixNano(),
		}
		if !entry.IsDir() {
			// #nosec G304,G122 -- path is produced by WalkDir below the test's private temporary root.
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
