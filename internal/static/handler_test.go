package static_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	staticserver "github.com/tanzir71/dropserve/internal/static"
)

func TestPathTraversalIsRefused(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	root := filepath.Join(sandbox, "apps", "safe")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("create app root: %v", err)
	}
	outsideFile := filepath.Join(sandbox, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("must not be served"), 0o600); err != nil {
		t.Fatalf("create outside file: %v", err)
	}

	cases := map[string]string{
		"parent":            "../secret.txt",
		"encoded slash":     "..%2fsecret.txt",
		"encoded backslash": "%2e%2e%5csecret.txt",
		"absolute":          outsideFile,
		"UNC":               `\\server\share\secret.txt`,
	}
	for name, requestedPath := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resolved, err := staticserver.Resolve(root, requestedPath)
			if !errors.Is(err, staticserver.ErrUnsafePath) {
				t.Fatalf("Resolve(%q) = %q, %v; want ErrUnsafePath", requestedPath, resolved, err)
			}
		})
	}

	t.Run("escaping symlink", func(t *testing.T) {
		link := filepath.Join(root, "outside-link.txt")
		if err := os.Symlink(outsideFile, link); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("Windows runner cannot create symlinks: %v", err)
			}
			t.Fatalf("create escaping symlink: %v", err)
		}

		resolved, err := staticserver.Resolve(root, "outside-link.txt")
		if !errors.Is(err, staticserver.ErrUnsafePath) {
			t.Fatalf("Resolve(symlink) = %q, %v; want ErrUnsafePath", resolved, err)
		}
	})
}
