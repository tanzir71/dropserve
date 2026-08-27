package server_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestCommandRedirectIsRewrittenUnderSlug(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the subpath fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "subpath"))
	if err != nil {
		t.Fatalf("resolve subpath fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create subpath server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close subpath server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	client := *httpServer.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/subpath/redirect",
		nil,
	)
	if err != nil {
		t.Fatalf("create redirect request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request redirect fixture: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("redirect status = %d, want 302", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/subpath/login" {
		t.Fatalf("redirect Location = %q, want /subpath/login", location)
	}
}

func TestCommandCookiePathIsRewrittenUnderSlug(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the subpath fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "subpath"))
	if err != nil {
		t.Fatalf("resolve subpath fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create subpath server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close subpath server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/subpath/cookie",
		nil,
	)
	if err != nil {
		t.Fatalf("create cookie request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("request cookie fixture: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "s" || cookies[0].Value != "1" {
		t.Fatalf("proxied cookies = %#v", cookies)
	}
	if cookies[0].Path != "/subpath/" {
		t.Fatalf("cookie Path = %q, want /subpath/", cookies[0].Path)
	}
}

func TestCommandHTMLBaseInjection(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the subpath fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "subpath"))
	if err != nil {
		t.Fatalf("resolve subpath fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create subpath server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close subpath server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	const withoutBase = "<!doctype html><html><head><title>No base</title></head><body>plain</body></html>"
	const wantInjected = "<!doctype html><html><head><base href=\"/subpath/\"><title>No base</title></head><body>plain</body></html>"
	status, injected := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/subpath/html-no-base")
	if status != http.StatusOK || injected != wantInjected {
		t.Fatalf("HTML without base = %d %q; source=%q", status, injected, withoutBase)
	}

	const withBase = "<!doctype html><html><head><base href=\"/custom/\"><title>Base</title></head><body>kept</body></html>"
	status, unchanged := requestCommandApp(t, httpServer.Client(), httpServer.URL+"/subpath/html-with-base")
	if status != http.StatusOK || unchanged != withBase {
		t.Fatalf("HTML with existing base changed: %d %q", status, unchanged)
	}
}

func TestCommandNonHTMLResponsesAreByteIdentical(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the subpath fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "subpath"))
	if err != nil {
		t.Fatalf("resolve subpath fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create subpath server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close subpath server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	tests := map[string][]byte{
		"asset.json": []byte(`{"markup":"<head>json</head>"}`),
		"asset.js":   []byte(`window.fixture = "<head>js</head>";`),
		"asset.css":  []byte(`body::before { content: "<head>css</head>"; }`),
		"asset.png":  {0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52},
	}
	for name, source := range tests {
		request, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			httpServer.URL+"/subpath/"+name,
			nil,
		)
		if err != nil {
			t.Fatalf("create %s request: %v", name, err)
		}
		response, err := httpServer.Client().Do(request)
		if err != nil {
			t.Fatalf("request %s: %v", name, err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if got, want := sha256.Sum256(body), sha256.Sum256(source); got != want {
			t.Fatalf("%s hash = %x, want byte-identical %x", name, got, want)
		}
	}
}

func TestFiveMegabyteHTMLResponseIsNotRewritten(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the subpath fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "subpath"))
	if err != nil {
		t.Fatalf("resolve subpath fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create subpath server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close subpath server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	prefix := []byte("<!doctype html><html><head><title>Large</title></head><body>")
	suffix := []byte("</body></html>")
	source := make([]byte, 0, 5<<20)
	source = append(source, prefix...)
	source = append(source, bytes.Repeat([]byte("x"), (5<<20)-len(prefix)-len(suffix))...)
	source = append(source, suffix...)
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/subpath/large-html",
		nil,
	)
	if err != nil {
		t.Fatalf("create large HTML request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("request large HTML: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read large HTML: %v", readErr)
	}
	if response.ContentLength != -1 || len(response.TransferEncoding) == 0 || response.TransferEncoding[0] != "chunked" {
		t.Fatalf("large fixture was not chunked: length=%d transfer=%v", response.ContentLength, response.TransferEncoding)
	}
	if len(body) != 5<<20 {
		t.Fatalf("large HTML bytes = %d, want 5 MB", len(body))
	}
	if got, want := sha256.Sum256(body), sha256.Sum256(source); got != want {
		t.Fatalf("large HTML hash = %x, want unchanged %x", got, want)
	}
}

func TestCommandReceivesForwardedSubpathHeaders(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; CI installs Node so this acceptance test runs there")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed; the subpath fixture requires it")
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "subpath"))
	if err != nil {
		t.Fatalf("resolve subpath fixture: %v", err)
	}
	server, err := dropserver.New(scanner.Options{Registered: []string{fixture}})
	if err != nil {
		t.Fatalf("create subpath server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close subpath server: %v", closeErr)
		}
	}()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		httpServer.URL+"/subpath/headers",
		nil,
	)
	if err != nil {
		t.Fatalf("create forwarded-header request: %v", err)
	}
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("request forwarded headers: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	var headers struct {
		Prefix     string `json:"prefix"`
		ScriptName string `json:"scriptName"`
		Host       string `json:"host"`
		Proto      string `json:"proto"`
	}
	if err := json.NewDecoder(response.Body).Decode(&headers); err != nil {
		t.Fatalf("decode forwarded headers: %v", err)
	}
	if headers.Prefix != "/subpath" || headers.ScriptName != "/subpath" {
		t.Fatalf("forwarded prefix headers = %#v", headers)
	}
	if headers.Host != request.URL.Host || headers.Proto != "http" {
		t.Fatalf("forwarded public origin = %#v, want host %q and http", headers, request.URL.Host)
	}
}
