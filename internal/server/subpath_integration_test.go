package server_test

import (
	"context"
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
