package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

func TestConfiguredAppCanOwnRootWithoutShadowingDashboardOrOtherApps(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"landing": "pinned root", "notes": "other app"} {
		appRoot := filepath.Join(root, name)
		if err := os.Mkdir(appRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "landing", "asset.txt"), []byte("root asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := dropserver.NewWithOptions(dropserver.Options{Scanner: scanner.Options{Roots: []string{root}}, PinToRoot: "landing"})
	if err != nil {
		t.Fatalf("create pinned-root server: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/", want: "pinned root"},
		{path: "/asset.txt", want: "root asset"},
		{path: "/notes/", want: "other app"},
		{path: "/_dropserve/", want: "What do you want to open?"},
	} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://dropserve.test"+test.path, nil)
		request.RemoteAddr = "127.0.0.1:5000"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("GET %s = %d %q, want body containing %q", test.path, response.Code, response.Body.String(), test.want)
		}
	}
}
