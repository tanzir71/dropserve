package scanner_test

import (
	"os"
	"path/filepath"
	"regexp"
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
