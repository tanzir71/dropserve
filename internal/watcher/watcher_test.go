package watcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWatchDepthAndPerAppBudgetAreBounded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	appRoot := filepath.Join(root, "bounded-app")
	deepest := filepath.Join(appRoot, "level-1", "level-2", "level-3", "level-4")
	if err := os.MkdirAll(deepest, 0o750); err != nil {
		t.Fatalf("create deep app tree: %v", err)
	}
	ignored := filepath.Join(appRoot, "node_modules", "dependency")
	if err := os.MkdirAll(ignored, 0o750); err != nil {
		t.Fatalf("create ignored dependency tree: %v", err)
	}
	for index := range 8 {
		if err := os.Mkdir(filepath.Join(appRoot, "wide-"+string(rune('a'+index))), 0o750); err != nil {
			t.Fatalf("create wide directory %d: %v", index, err)
		}
	}

	liveWatcher, err := New(Options{
		Roots:         []string{root},
		MaximumDepth:  3,
		WatchesPerApp: 4,
		Reconcile:     func() error { return nil },
	})
	if err != nil {
		t.Fatalf("create bounded watcher: %v", err)
	}
	defer func() {
		if closeErr := liveWatcher.Close(); closeErr != nil {
			t.Errorf("close bounded watcher: %v", closeErr)
		}
	}()

	appWatchCount := 0
	for path := range liveWatcher.watched {
		relative, relativeErr := filepath.Rel(appRoot, path)
		if relativeErr == nil && relative != ".." && !filepath.IsAbs(relative) {
			appWatchCount++
		}
		if path == filepath.Join(appRoot, "level-1", "level-2", "level-3", "level-4") {
			t.Fatal("watcher exceeded the three-level recursion cap")
		}
		if path == ignored || filepath.Dir(path) == ignored {
			t.Fatal("watcher entered node_modules")
		}
	}
	if appWatchCount != 4 {
		t.Fatalf("app watch count = %d, want the configured budget of 4", appWatchCount)
	}
}
