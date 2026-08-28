// Package static serves files from a discovered static app without modifying it.
package static

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
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
		serveNotFound(response, request)
		return
	}

	file, info, ok := openRegularFile(resolved)
	if !ok {
		if server.serveSPAFallback(response, request) {
			return
		}
		serveNotFound(response, request)
		return
	}
	if info.IsDir() {
		if server.application.Index == "" {
			defer func() {
				_ = file.Close()
			}()
			if !server.application.DirectoryListing {
				serveNotFound(response, request)
				return
			}
			serveDirectory(response, request, file)
			return
		}
		_ = file.Close()
		resolved, err = Resolve(server.application.Path, path.Join(relativePath, server.application.Index))
		if err != nil {
			serveNotFound(response, request)
			return
		}
		file, info, ok = openRegularFile(resolved)
		if !ok || info.IsDir() {
			if ok {
				_ = file.Close()
			}
			if server.serveSPAFallback(response, request) {
				return
			}
			serveNotFound(response, request)
			return
		}
	}
	defer func() {
		_ = file.Close()
	}()
	serveFile(response, request, info, file)
}

func (server *handler) serveSPAFallback(response http.ResponseWriter, request *http.Request) bool {
	if !server.application.SPA || server.application.Index == "" {
		return false
	}
	resolved, err := Resolve(server.application.Path, server.application.Index)
	if err != nil {
		return false
	}
	file, info, ok := openRegularFile(resolved)
	if !ok || info.IsDir() {
		if ok {
			_ = file.Close()
		}
		return false
	}
	defer func() { _ = file.Close() }()
	serveFile(response, request, info, file)
	return true
}

func (server *handler) serveLooseFile(response http.ResponseWriter, request *http.Request) {
	if strings.Trim(request.URL.Path, "/") != "" {
		serveNotFound(response, request)
		return
	}
	// #nosec G304,G122 -- the path is the read-only app file captured by the scanner, not request input.
	file, err := os.Open(server.application.Path)
	if err != nil {
		serveNotFound(response, request)
		return
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		serveNotFound(response, request)
		return
	}
	serveFile(response, request, info, file)
}

func serveNotFound(response http.ResponseWriter, request *http.Request) {
	const content = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>App not found · Dropserve</title><body><main><h1>Dropserve could not find that app or file.</h1><p>Check the address, or return to <a href="/">your apps</a>.</p></main></body></html>`
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusNotFound)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(response, content)
}

func serveFile(response http.ResponseWriter, request *http.Request, info os.FileInfo, file *os.File) {
	etag := fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size())
	response.Header().Set("ETag", etag)
	if matchesETag(request.Header.Get("If-None-Match"), etag) {
		if request.Method == http.MethodGet || request.Method == http.MethodHead {
			response.WriteHeader(http.StatusNotModified)
		} else {
			response.WriteHeader(http.StatusPreconditionFailed)
		}
		return
	}
	http.ServeContent(response, request, info.Name(), info.ModTime(), file)
}

func matchesETag(condition, etag string) bool {
	for _, candidate := range strings.Split(condition, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func serveDirectory(response http.ResponseWriter, request *http.Request, directory *os.File) {
	entries, err := directory.ReadDir(-1)
	if err != nil {
		http.Error(response, "Dropserve could not read this directory.", http.StatusInternalServerError)
		return
	}
	sort.Slice(entries, func(first, second int) bool {
		return strings.ToLower(entries[first].Name()) < strings.ToLower(entries[second].Name())
	})

	var body strings.Builder
	body.WriteString("<!doctype html><meta charset=\"utf-8\"><title>Directory listing</title>")
	body.WriteString("<h1>Directory listing</h1><ul>")
	for _, entry := range entries {
		name := entry.Name()
		href := url.PathEscape(name)
		if entry.IsDir() {
			name += "/"
			href += "/"
		}
		_, _ = fmt.Fprintf(&body, `<li><a href="%s">%s</a></li>`, href, html.EscapeString(name))
	}
	body.WriteString("</ul>")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method == http.MethodHead {
		response.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(response, body.String())
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
