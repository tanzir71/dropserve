package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/scanner"
)

func TestServerLifecycleNeverInstallsTrustWithoutExplicitAction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Apps")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("create Apps root: %v", err)
	}
	trustCalls := 0
	server, err := NewWithOptions(Options{
		Scanner:          scanner.Options{Roots: []string{root}},
		LocalHTTPSStatus: func() dashboard.LocalHTTPSStatus { return dashboard.LocalHTTPSStatus{} },
		SetLocalHTTPS:    func(context.Context, bool) error { return nil },
		SetLocalTrust: func(bool) error {
			trustCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard response = %d", response.Code)
	}
	if err := server.Reconcile(); err != nil {
		t.Fatalf("reconcile server: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	if trustCalls != 0 {
		t.Fatalf("server startup/scan/serve/shutdown made %d trust-store calls, want zero", trustCalls)
	}
}
