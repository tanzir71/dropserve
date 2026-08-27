package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tanzir71/dropserve/internal/app"
)

func TestIndexCacheRoundTripsAtomically(t *testing.T) {
	t.Parallel()

	appRoot := filepath.Join(t.TempDir(), "searchable")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatalf("create app: %v", err)
	}
	writeIndexFixtureFile(t, filepath.Join(appRoot, "index.html"), []byte("<title>Searchable</title>"))
	writeIndexFixtureFile(t, filepath.Join(appRoot, "private-keyword.txt"), []byte("content"))
	entries := Build([]app.App{{
		Slug:  "searchable",
		Name:  "Searchable",
		Path:  appRoot,
		Kind:  app.KindStatic,
		Index: "index.html",
	}})
	cacheDirectory := t.TempDir()
	cachePath := filepath.Join(cacheDirectory, "index.json")
	if err := Save(cachePath, entries); err != nil {
		t.Fatalf("save index cache: %v", err)
	}
	loaded, err := Load(cachePath)
	if err != nil {
		t.Fatalf("load index cache: %v", err)
	}
	if !reflect.DeepEqual(loaded, entries) {
		t.Fatalf("cache round trip changed entries\nloaded: %#v\nwant:   %#v", loaded, entries)
	}

	entries[0].Description = "updated"
	if err := Save(cachePath, entries); err != nil {
		t.Fatalf("replace index cache: %v", err)
	}
	loaded, err = Load(cachePath)
	if err != nil {
		t.Fatalf("load replaced cache: %v", err)
	}
	if loaded[0].Description != "updated" {
		t.Fatalf("replacement description = %q, want updated", loaded[0].Description)
	}
	files, err := os.ReadDir(cacheDirectory)
	if err != nil {
		t.Fatalf("read cache directory: %v", err)
	}
	if len(files) != 1 || files[0].Name() != "index.json" {
		t.Fatalf("atomic save left unexpected files: %v", files)
	}
}
