package indexer

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

func TestCloudPlaceholderIsNamedButNeverOpened(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	placeholder := filepath.Join(root, "cloud-only-blueprint.html")
	writeIndexFixtureFile(t, placeholder, []byte("<h1>This content must not be hydrated</h1>"))
	access := &recordingFileAccess{placeholder: filepath.Clean(placeholder)}
	entries := BuildWithOptions([]app.App{{
		Slug:  "cloud-plan",
		Name:  "Cloud plan",
		Path:  root,
		Kind:  app.KindStatic,
		Index: filepath.Base(placeholder),
	}}, BuildOptions{Files: access})

	if access.placeholderChecks == 0 {
		t.Fatal("injected cloud-placeholder stat interface was not consulted")
	}
	if runtime.GOOS != "windows" {
		return
	}
	if access.openedPlaceholder {
		t.Fatal("indexer opened a cloud-only placeholder")
	}
	results := Search(entries, "cloud-only-blueprint")
	if len(results) != 1 || results[0].Slug != "cloud-plan" {
		t.Fatalf("placeholder filename search = %#v, want cloud-plan", results)
	}
	if entries[0].Title != "" || entries[0].Heading != "" {
		t.Fatalf("placeholder HTML was read: title=%q heading=%q", entries[0].Title, entries[0].Heading)
	}
}

type recordingFileAccess struct {
	placeholder       string
	placeholderChecks int
	openedPlaceholder bool
}

func (access *recordingFileAccess) Open(path string) (io.ReadCloser, error) {
	if filepath.Clean(path) == access.placeholder {
		access.openedPlaceholder = true
		return nil, errors.New("cloud placeholder must not be opened")
	}
	// #nosec G304 -- the test adapter receives paths created under t.TempDir.
	return os.Open(path)
}

func (access *recordingFileAccess) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (access *recordingFileAccess) IsCloudPlaceholder(path string, _ os.FileInfo) (bool, error) {
	access.placeholderChecks++
	return runtime.GOOS == "windows" && filepath.Clean(path) == access.placeholder, nil
}

func writeIndexFixtureFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}
