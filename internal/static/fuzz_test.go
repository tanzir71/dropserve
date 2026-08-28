package static_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	staticserver "github.com/tanzir71/dropserve/internal/static"
)

func FuzzPathResolver(f *testing.F) {
	sandbox := f.TempDir()
	root := filepath.Join(sandbox, "app")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o750); err != nil {
		f.Fatalf("create fuzz app root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "safe.txt"), []byte("safe"), 0o600); err != nil {
		f.Fatalf("create fuzz app file: %v", err)
	}
	outside := filepath.Join(sandbox, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		f.Fatalf("create outside fuzz file: %v", err)
	}
	seeds := []string{
		"",
		"assets/safe.txt",
		"../outside.txt",
		"..%2foutside.txt",
		"%2e%2e%5coutside.txt",
		outside,
		`\\server\share\file.txt`,
		"%00",
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape-link")); err == nil {
		seeds = append(seeds, "escape-link")
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		f.Fatalf("resolve fuzz app root: %v", err)
	}
	f.Fuzz(func(t *testing.T, requestedPath string) {
		resolved, resolveErr := staticserver.Resolve(root, requestedPath)
		if resolveErr != nil {
			return
		}
		relative, relErr := filepath.Rel(resolvedRoot, resolved)
		if relErr != nil {
			t.Fatalf("relate resolved path %q to root %q: %v", resolved, resolvedRoot, relErr)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			t.Fatalf("Resolve(%q) escaped root %q as %q", requestedPath, resolvedRoot, resolved)
		}
	})
}
