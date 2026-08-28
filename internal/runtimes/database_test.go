package runtimes

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDatabaseDataDirectoriesAreAlwaysUnderState(t *testing.T) {
	sandbox := t.TempDir()
	stateDirectory := filepath.Join(sandbox, "state")
	appsRoot := filepath.Join(sandbox, "Apps")
	appPath := filepath.Join(appsRoot, "inventory")
	if err := os.MkdirAll(appPath, 0o750); err != nil {
		t.Fatalf("create app fixture: %v", err)
	}
	marker := filepath.Join(appPath, "index.html")
	if err := os.WriteFile(marker, []byte("unchanged app"), 0o600); err != nil {
		t.Fatalf("write app fixture: %v", err)
	}
	before, err := os.ReadDir(appPath)
	if err != nil {
		t.Fatalf("read app fixture before database setup: %v", err)
	}

	for _, engine := range []string{"mariadb", "postgres"} {
		directory, err := DatabaseDataDirectory(stateDirectory, engine)
		if err != nil {
			t.Fatalf("prepare %s data directory: %v", engine, err)
		}
		want := filepath.Join(stateDirectory, "databases", engine, "data")
		if directory != want {
			t.Errorf("%s data directory = %q, want %q", engine, directory, want)
		}
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			t.Errorf("%s data directory was not created: info=%v err=%v", engine, info, err)
		}
		relativeToApps, err := filepath.Rel(appsRoot, directory)
		if err == nil && relativeToApps != ".." && !filepath.IsAbs(relativeToApps) && !strings.HasPrefix(relativeToApps, ".."+string(filepath.Separator)) {
			t.Errorf("%s data directory was created under Apps root: %s", engine, directory)
		}
	}
	after, err := os.ReadDir(appPath)
	if err != nil {
		t.Fatalf("read app fixture after database setup: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("app fixture directory changed: before=%v after=%v", before, after)
	}
	content, err := os.ReadFile(marker) // #nosec G304 -- marker is inside this test's private app fixture.
	if err != nil || string(content) != "unchanged app" {
		t.Fatalf("app fixture content changed: content=%q err=%v", content, err)
	}
}
