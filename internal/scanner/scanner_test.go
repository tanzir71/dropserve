package scanner_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
)

func TestSlugSanitisation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"My Notes", "Ünïcødé Tool", "..evil", "_scratch", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatalf("create fixture %q: %v", name, err)
		}
	}

	result, err := scanner.Scan(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}

	slugsByName := make(map[string]string, len(result.Apps))
	for _, application := range result.Apps {
		slugsByName[application.Name] = application.Slug
	}
	if got, want := slugsByName["My Notes"], "my-notes"; got != want {
		t.Fatalf("My Notes slug = %q, want %q", got, want)
	}

	unicodeSlug := slugsByName["Ünïcødé Tool"]
	if unicodeSlug == "" {
		t.Fatal("Unicode name did not produce a slug")
	}
	if !regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`).MatchString(unicodeSlug) {
		t.Fatalf("Unicode slug %q is not stable ASCII", unicodeSlug)
	}
	if repeated := scanner.Slug("Ünïcødé Tool"); repeated != unicodeSlug {
		t.Fatalf("Unicode slug changed between calls: %q then %q", unicodeSlug, repeated)
	}

	for _, ignored := range []string{"evil", "scratch", "hidden"} {
		if _, mounted := slugsByName[ignored]; mounted {
			t.Fatalf("%q was mounted but should have been rejected or ignored", ignored)
		}
	}
	if got := scanner.Slug("..evil"); got != "" {
		t.Fatalf("Slug(..evil) = %q, want rejection", got)
	}

	foundUnsafeWarning := false
	for _, warning := range result.Warnings {
		if filepath.Base(warning.Path) == "..evil" && warning.Code == "unsafe_name" {
			foundUnsafeWarning = true
		}
		if base := filepath.Base(warning.Path); base == "_scratch" || base == ".hidden" {
			t.Fatalf("ignored app %q produced a warning", base)
		}
	}
	if !foundUnsafeWarning {
		t.Fatal("..evil was not rejected with an unsafe_name warning")
	}
}

func TestCaseInsensitiveCollisionAndRename(t *testing.T) {
	t.Parallel()

	t.Run("collision", func(t *testing.T) {
		firstRoot := t.TempDir()
		secondRoot := t.TempDir()
		for _, fixture := range []struct {
			root string
			name string
		}{
			{firstRoot, "Notes"},
			{secondRoot, "notes"},
		} {
			if err := os.Mkdir(filepath.Join(fixture.root, fixture.name), 0o750); err != nil {
				t.Fatalf("create %s: %v", fixture.name, err)
			}
		}

		result, err := scanner.Scan(scanner.Options{Roots: []string{firstRoot, secondRoot}})
		if err != nil {
			t.Fatalf("scan roots: %v", err)
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
	})

	t.Run("case-only rename", func(t *testing.T) {
		root := t.TempDir()
		lowerPath := filepath.Join(root, "notes")
		if err := os.Mkdir(lowerPath, 0o750); err != nil {
			t.Fatalf("create notes: %v", err)
		}
		before, err := scanner.Scan(scanner.Options{Roots: []string{root}})
		if err != nil {
			t.Fatalf("scan before rename: %v", err)
		}

		intermediatePath := filepath.Join(root, "rename-in-progress")
		titlePath := filepath.Join(root, "Notes")
		if err := os.Rename(lowerPath, intermediatePath); err != nil {
			t.Fatalf("rename through intermediate path: %v", err)
		}
		if err := os.Rename(intermediatePath, titlePath); err != nil {
			t.Fatalf("complete case-only rename: %v", err)
		}
		after, err := scanner.Scan(scanner.Options{Roots: []string{root}})
		if err != nil {
			t.Fatalf("scan after rename: %v", err)
		}

		changes := scanner.Compare(before, after, true)
		if len(changes.Renamed) != 1 {
			t.Fatalf("renamed changes = %d, want 1: %#v", len(changes.Renamed), changes)
		}
		if len(changes.Added) != 0 || len(changes.Removed) != 0 {
			t.Fatalf("case-only rename produced added/removed changes: %#v", changes)
		}
		rename := changes.Renamed[0]
		if filepath.Base(rename.Before.Path) != "notes" || filepath.Base(rename.After.Path) != "Notes" {
			t.Fatalf("rename = %q -> %q, want notes -> Notes", rename.Before.Path, rename.After.Path)
		}
	})
}

func TestScannerWalksLongPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	appRoot := filepath.Join(root, "deep-tree")
	deepDirectory := appRoot
	for len(filepath.Join(deepDirectory, "payload.txt")) <= 280 {
		deepDirectory = filepath.Join(deepDirectory, "segment-"+strings.Repeat("x", 24))
	}
	if err := os.MkdirAll(deepDirectory, 0o750); err != nil {
		t.Fatalf("create deep fixture tree: %v", err)
	}
	deepFile := filepath.Join(deepDirectory, "payload.txt")
	if len(deepFile) <= 260 {
		t.Fatalf("fixture path length = %d, want > 260", len(deepFile))
	}
	if err := os.WriteFile(deepFile, []byte("deep path"), 0o600); err != nil {
		t.Fatalf("write deep fixture: %v", err)
	}

	result, err := scanner.Scan(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("scan deep fixture: %v", err)
	}
	if len(result.Apps) != 1 {
		t.Fatalf("scan returned %d apps, want 1", len(result.Apps))
	}
	if got, want := result.Apps[0].FileCount, int64(1); got != want {
		t.Fatalf("deep-tree file count = %d, want %d", got, want)
	}
}

func TestSyncRootWarningNamesAllKnownProviders(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"OneDrive", "Dropbox", "Google Drive", "iCloud Drive"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), provider, "Apps")
			if err := os.MkdirAll(root, 0o750); err != nil {
				t.Fatalf("create %s root: %v", provider, err)
			}
			result, err := scanner.Scan(scanner.Options{Roots: []string{root}})
			if err != nil {
				t.Fatalf("scan %s root: %v", provider, err)
			}
			warnings := make([]string, 0, len(result.Warnings))
			for _, warning := range result.Warnings {
				warnings = append(warnings, warning.Message)
			}
			combined := strings.Join(warnings, "\n")
			if !strings.Contains(combined, root) || !strings.Contains(combined, provider) ||
				!strings.Contains(combined, `%USERPROFILE%\Dropserve`) {
				t.Fatalf("%s warnings = %q", provider, combined)
			}
		})
	}
}

func TestReservedSlugsAreRefusedWithWarnings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reserved := []string{"_dropserve", "api", "health", ".well-known"}
	for _, name := range reserved {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatalf("create reserved fixture %q: %v", name, err)
		}
	}

	result, err := scanner.Scan(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("scan reserved fixtures: %v", err)
	}
	if len(result.Apps) != 0 {
		t.Fatalf("reserved scan mounted %d apps, want 0: %#v", len(result.Apps), result.Apps)
	}

	warned := make(map[string]bool, len(result.Warnings))
	for _, warning := range result.Warnings {
		if warning.Code == "reserved_slug" {
			warned[filepath.Base(warning.Path)] = true
		}
	}
	for _, name := range reserved {
		if !warned[name] {
			t.Errorf("reserved name %q did not produce a reserved_slug warning", name)
		}
	}
}

func TestStaticManifestControlsIndexPresentationAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "plain-folder")
	if err := os.MkdirAll(filepath.Join(appRoot, "web"), 0o750); err != nil {
		t.Fatalf("create app fixture: %v", err)
	}
	manifest := `{
		"name":"Manifest Name","description":"Manifest description","icon":"🏛️","tags":["reference"],
		"type":"static","index":"web/home.html","spa":true,"directory_listing":false,
		"visibility":"local","pinned":true,"hidden":true,"mystery":1
	}`
	if err := os.WriteFile(filepath.Join(appRoot, "dropserve.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "web", "home.html"), []byte("manifest home"), 0o600); err != nil {
		t.Fatalf("write manifest index: %v", err)
	}

	result, err := scanner.Scan(scanner.Options{Roots: []string{root}})
	if err != nil {
		t.Fatalf("scan manifest app: %v", err)
	}
	if len(result.Apps) != 1 {
		t.Fatalf("apps = %#v", result.Apps)
	}
	application := result.Apps[0]
	if application.Name != "Manifest Name" || application.Description != "Manifest description" || application.Icon != "🏛️" || strings.Join(application.Tags, ",") != "reference" {
		t.Fatalf("presentation = %#v", application)
	}
	if application.Index != "web/home.html" || !application.SPA || application.DirectoryListing || application.Visibility != "local" || !application.Pinned || !application.Hidden {
		t.Fatalf("static settings = %#v", application)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != "manifest_unknown_key" || !strings.Contains(result.Warnings[0].Message, "mystery") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestGlobalLazyStartStopsCommandAppsUntilFirstRequest(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "node-app")
	if err := os.Mkdir(appRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(scanner.Options{Roots: []string{root}, LazyStartCommands: true})
	if err != nil {
		t.Fatalf("scan lazy app: %v", err)
	}
	if len(result.Apps) != 1 || result.Apps[0].Autostart {
		t.Fatalf("lazy scan apps = %#v", result.Apps)
	}
}
