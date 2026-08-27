package static_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/app"
	staticserver "github.com/tanzir71/dropserve/internal/static"
)

func TestStaticFileValidatorsAndRange(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	contents := []byte("<!doctype html><h1>Static fixture</h1>")
	if err := os.WriteFile(filepath.Join(root, "index.html"), contents, 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	handler := staticserver.New(app.App{Path: root, Index: "index.html"})

	initial := serveStaticRequest(t, handler, http.MethodGet, "/", nil)
	if initial.StatusCode != http.StatusOK {
		t.Fatalf("initial status = %d, want 200", initial.StatusCode)
	}
	if contentType := initial.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("content type = %q, want text/html", contentType)
	}
	etag := initial.Header.Get("ETag")
	if etag == "" {
		t.Fatal("initial response has no ETag")
	}

	notModified := serveStaticRequest(t, handler, http.MethodGet, "/", map[string]string{
		"If-None-Match": etag,
	})
	if notModified.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", notModified.StatusCode)
	}
	if len(notModified.Body) != 0 {
		t.Fatalf("conditional body = %q, want empty", notModified.Body)
	}

	partial := serveStaticRequest(t, handler, http.MethodGet, "/", map[string]string{
		"Range": "bytes=0-4",
	})
	if partial.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", partial.StatusCode)
	}
	if !bytes.Equal(partial.Body, contents[:5]) {
		t.Fatalf("range body = %q, want %q", partial.Body, contents[:5])
	}
}

func TestDirectoryListingWhenNoIndexExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha note.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatalf("write listing file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o750); err != nil {
		t.Fatalf("create listing folder: %v", err)
	}
	handler := staticserver.New(app.App{Path: root, DirectoryListing: true})

	response := serveStaticRequest(t, handler, http.MethodGet, "/", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("listing status = %d, want 200; body=%q", response.StatusCode, response.Body)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("listing content type = %q, want text/html", contentType)
	}
	body := string(response.Body)
	for _, expected := range []string{"alpha note.txt", `href="alpha%20note.txt"`, `href="folder/"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("listing body does not contain %q: %s", expected, body)
		}
	}
}

type staticResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func serveStaticRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	headers map[string]string,
) staticResponse {
	t.Helper()

	request := httptest.NewRequestWithContext(context.Background(), method, "http://dropserve.test"+path, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	defer func() {
		_ = result.Body.Close()
	}()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return staticResponse{StatusCode: result.StatusCode, Header: result.Header, Body: body}
}
