// Package static serves files from a discovered static app without modifying it.
package static

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/tanzir71/dropserve/internal/app"
)

// ErrUnsafePath means a request tried to escape or reinterpret the app root.
var ErrUnsafePath = errors.New("requested path is outside the app root")

type handler struct {
	application app.App
}

// New returns a handler for a static app.
func New(application app.App) http.Handler {
	return &handler{application: application}
}

// Resolve converts one URL-relative path into a filesystem path and proves that
// its lexical path, symlink target, and nearest existing ancestor remain below
// root. Both slash styles are treated as separators on every platform.
func Resolve(root, requestedPath string) (string, error) {
	decoded, err := url.PathUnescape(requestedPath)
	if err != nil {
		return "", fmt.Errorf("%w: invalid URL escaping", ErrUnsafePath)
	}
	if strings.ContainsRune(decoded, 0) {
		return "", fmt.Errorf("%w: zero byte", ErrUnsafePath)
	}

	normalized := strings.ReplaceAll(decoded, `\`, "/")
	if filesystemAbsolute(decoded, normalized) {
		return "", fmt.Errorf("%w: absolute path", ErrUnsafePath)
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." {
		cleaned = ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: parent traversal", ErrUnsafePath)
	}

	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve app root: %w", err)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbsolute)
	if err != nil {
		return "", fmt.Errorf("resolve app root symlinks: %w", err)
	}
	candidate := filepath.Join(rootAbsolute, filepath.FromSlash(cleaned))
	candidateAbsolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve requested path: %w", err)
	}
	if !within(rootAbsolute, candidateAbsolute) {
		return "", fmt.Errorf("%w: lexical escape", ErrUnsafePath)
	}

	candidateResolved, err := resolveThroughExistingAncestor(candidateAbsolute)
	if err != nil {
		return "", fmt.Errorf("resolve requested path symlinks: %w", err)
	}
	if !within(rootResolved, candidateResolved) {
		return "", fmt.Errorf("%w: symlink escape", ErrUnsafePath)
	}
	return candidateResolved, nil
}

func (server *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if server.application.LooseFile {
		server.serveLooseFile(response, request)
		return
	}

	relativePath := strings.TrimPrefix(request.URL.Path, "/")
	resolved, err := Resolve(server.application.Path, relativePath)
	if err != nil {
		http.NotFound(response, request)
		return
	}

	file, info, ok := openRegularFile(resolved)
	if !ok {
		http.NotFound(response, request)
		return
	}
	if info.IsDir() {
		_ = file.Close()
		if server.application.Index == "" {
			http.NotFound(response, request)
			return
		}
		resolved, err = Resolve(server.application.Path, path.Join(relativePath, server.application.Index))
		if err != nil {
			http.NotFound(response, request)
			return
		}
		file, info, ok = openRegularFile(resolved)
		if !ok || info.IsDir() {
			if ok {
				_ = file.Close()
			}
			http.NotFound(response, request)
			return
		}
	}
	defer func() {
		_ = file.Close()
	}()
	http.ServeContent(response, request, info.Name(), info.ModTime(), file)
}

func (server *handler) serveLooseFile(response http.ResponseWriter, request *http.Request) {
	if strings.Trim(request.URL.Path, "/") != "" {
		http.NotFound(response, request)
		return
	}
	// #nosec G304,G122 -- the path is the read-only app file captured by the scanner, not request input.
	file, err := os.Open(server.application.Path)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(response, request)
		return
	}
	http.ServeContent(response, request, info.Name(), info.ModTime(), file)
}

func openRegularFile(resolved string) (*os.File, os.FileInfo, bool) {
	// #nosec G304,G122 -- Resolve proves this path and its symlink ancestry remain under the app root.
	file, err := os.Open(resolved)
	if err != nil {
		return nil, nil, false
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, false
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, false
	}
	return file, info, true
}

func filesystemAbsolute(decoded, normalized string) bool {
	if filepath.IsAbs(decoded) || path.IsAbs(normalized) {
		return true
	}
	return len(normalized) >= 2 &&
		((normalized[0] >= 'a' && normalized[0] <= 'z') || (normalized[0] >= 'A' && normalized[0] <= 'Z')) &&
		normalized[1] == ':'
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func resolveThroughExistingAncestor(candidate string) (string, error) {
	current := candidate
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
