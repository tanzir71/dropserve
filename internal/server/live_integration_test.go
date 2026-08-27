package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestDeletedFolderIsRemovedWithinTwoSeconds(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	appRoot := filepath.Join(root, "old-notes")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte("<h1>Old notes</h1>"), 0o600); err != nil {
		t.Fatalf("write app index: %v", err)
	}
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

	initialStatus, initialBody := requestLiveApp(t, httpServer.Client(), httpServer.URL+"/old-notes/")
	if initialStatus != http.StatusOK {
		t.Fatalf("initial app returned %d; body=%q", initialStatus, initialBody)
	}
	if err := os.RemoveAll(appRoot); err != nil {
		t.Fatalf("delete app folder: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		status, body := requestLiveApp(t, httpServer.Client(), httpServer.URL+"/old-notes/")
		if status == http.StatusNotFound && len(server.Scan().Apps) == 0 {
			if !strings.Contains(body, "Dropserve could not find that app") {
				t.Fatalf("not-found body is not friendly: %q", body)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("deleted app returned %d after two-second deadline; body=%q", status, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRenamedFolderChangesSlugWithinTwoSeconds(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	beforeRoot := filepath.Join(root, "draft-notes")
	if err := os.Mkdir(beforeRoot, 0o750); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	const appBody = "<h1>Renamed notes</h1>"
	if err := os.WriteFile(filepath.Join(beforeRoot, "index.html"), []byte(appBody), 0o600); err != nil {
		t.Fatalf("write app index: %v", err)
	}
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

	afterRoot := filepath.Join(root, "published-notes")
	if err := os.Rename(beforeRoot, afterRoot); err != nil {
		t.Fatalf("rename app folder: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		oldStatus, _ := requestLiveApp(t, httpServer.Client(), httpServer.URL+"/draft-notes/")
		newStatus, newBody := requestLiveApp(t, httpServer.Client(), httpServer.URL+"/published-notes/")
		current := server.Scan()
		if oldStatus == http.StatusNotFound && newStatus == http.StatusOK && newBody == appBody &&
			len(current.Apps) == 1 && current.Apps[0].Slug == "published-notes" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"rename did not settle before deadline: old=%d new=%d body=%q scan=%#v",
				oldStatus,
				newStatus,
				newBody,
				current.Apps,
			)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRapidChangesAreDebounced(t *testing.T) {
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
	baselineRebuilds := server.RebuildCount()

	for index := range 20 {
		name := fmt.Sprintf("burst-%02d", index)
		appRoot := filepath.Join(root, name)
		if err := os.Mkdir(appRoot, 0o750); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		body := fmt.Sprintf("<h1>%s</h1>", name)
		if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s index: %v", name, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	var stableSince time.Time
	var lastRebuilds uint64
	for {
		current := server.Scan()
		rebuilds := server.RebuildCount() - baselineRebuilds
		if len(current.Apps) == 20 {
			if rebuilds != lastRebuilds || stableSince.IsZero() {
				lastRebuilds = rebuilds
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= 600*time.Millisecond {
				if rebuilds > 3 {
					t.Fatalf("rapid change burst triggered %d rebuilds, want at most 3", rebuilds)
				}
				for _, application := range current.Apps {
					status, _ := requestLiveApp(t, httpServer.Client(), httpServer.URL+"/"+application.Slug+"/")
					if status != http.StatusOK {
						t.Fatalf("settled route %s returned %d", application.Slug, status)
					}
				}
				return
			}
		} else {
			stableSince = time.Time{}
			lastRebuilds = rebuilds
		}
		if time.Now().After(deadline) {
			t.Fatalf("burst did not settle: apps=%d rebuilds=%d", len(current.Apps), rebuilds)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReconcileCatchesChangesWhileWatcherIsStopped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	server, err := dropserver.New(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("create live server: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("stop live watcher: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	appRoot := filepath.Join(root, "recovered-notes")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create app folder: %v", err)
	}
	const body = "<h1>Recovered notes</h1>"
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(body), 0o600); err != nil {
		t.Fatalf("write app index: %v", err)
	}
	if current := server.Scan(); len(current.Apps) != 0 {
		t.Fatalf("stopped watcher rebuilt unexpectedly: %#v", current.Apps)
	}

	if err := server.Reconcile(); err != nil {
		t.Fatalf("reconcile stopped watcher change: %v", err)
	}
	status, gotBody := requestLiveApp(t, httpServer.Client(), httpServer.URL+"/recovered-notes/")
	if status != http.StatusOK || gotBody != body {
		t.Fatalf("reconciled app returned %d body=%q", status, gotBody)
	}
	current := server.Scan()
	if len(current.Apps) != 1 || current.Apps[0].Slug != "recovered-notes" {
		t.Fatalf("reconciled snapshot = %#v", current.Apps)
	}
}

func requestLiveApp(t *testing.T, client *http.Client, target string) (int, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("create app request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request live app: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read live app: %v", err)
	}
	return response.StatusCode, string(body)
}
