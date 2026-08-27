package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/app"
)

func TestBuildExtractsDashboardMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	index := []byte("<!doctype html><title>Paper &amp; Trail</title><h1>Expense <em>archive</em></h1>")
	readme := []byte("# Paper Trail\n\nKeeps receipts organised.\n")
	favicon := []byte("fixture-icon")
	writeIndexFixtureFile(t, filepath.Join(root, "index.html"), index)
	writeIndexFixtureFile(t, filepath.Join(root, "README.md"), readme)
	writeIndexFixtureFile(t, filepath.Join(root, "favicon.ico"), favicon)
	stableTime := time.Unix(1_700_000_000, 0)
	newestTime := stableTime.Add(time.Hour)
	for _, name := range []string{"index.html", "README.md"} {
		if err := os.Chtimes(filepath.Join(root, name), stableTime, stableTime); err != nil {
			t.Fatalf("set %s time: %v", name, err)
		}
	}
	if err := os.Chtimes(filepath.Join(root, "favicon.ico"), newestTime, newestTime); err != nil {
		t.Fatalf("set favicon time: %v", err)
	}

	entries := Build([]app.App{{
		Slug:  "paper-trail",
		Name:  "Paper Trail",
		Path:  root,
		Kind:  app.KindStatic,
		Index: "index.html",
	}})
	if len(entries) != 1 {
		t.Fatalf("Build returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Title != "Paper & Trail" || entry.Heading != "Expense archive" {
		t.Fatalf("HTML metadata = title %q heading %q", entry.Title, entry.Heading)
	}
	wantSize := int64(len(index) + len(readme) + len(favicon))
	if entry.Size != wantSize {
		t.Fatalf("size = %d, want %d", entry.Size, wantSize)
	}
	if entry.MTime != newestTime.UnixMilli() {
		t.Fatalf("mtime = %d, want %d", entry.MTime, newestTime.UnixMilli())
	}
	if entry.Icon != "/paper-trail/favicon.ico" || entry.IconKind != "image" {
		t.Fatalf("favicon metadata = icon %q kind %q", entry.Icon, entry.IconKind)
	}
}

func TestBuildGeneratesDeterministicMonogram(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeIndexFixtureFile(t, filepath.Join(root, "index.html"), []byte("<h1>Notes</h1>"))
	application := app.App{
		Slug:  "notes-tool",
		Name:  "Notes Tool",
		Path:  root,
		Kind:  app.KindStatic,
		Index: "index.html",
	}
	first := Build([]app.App{application})[0]
	second := Build([]app.App{application})[0]
	if first.Icon != "NT" || first.IconKind != "monogram" {
		t.Fatalf("monogram metadata = icon %q kind %q", first.Icon, first.IconKind)
	}
	if first.IconColor == "" || !strings.HasPrefix(first.IconColor, "#") || first.IconColor != second.IconColor {
		t.Fatalf("icon colour is not deterministic: first %q second %q", first.IconColor, second.IconColor)
	}
}

func writeIndexFixtureFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}
