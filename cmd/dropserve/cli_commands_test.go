package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/config"
)

func TestUsageExposesTheHandoverCLIShape(t *testing.T) {
	for _, command := range []string{
		"dropserve serve", "dropserve status", "dropserve open", "dropserve apps",
		"dropserve add", "dropserve logs", "dropserve restart", "dropserve autostart",
		"dropserve trust", "dropserve firewall", "dropserve tailscale", "dropserve runtime",
		"dropserve config", "dropserve doctor", "dropserve version",
	} {
		if !strings.Contains(usage, command) {
			t.Errorf("help omits %q", command)
		}
	}
}

func TestLocalAPIUsesLiveCSRFBoundaryForHeadlessActions(t *testing.T) {
	const token = "test-token"
	var mutations []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/_dropserve/api/status":
			_ = json.NewEncoder(response).Encode(map[string]string{"csrf_token": token})
		case "/_dropserve/api/apps":
			_ = json.NewEncoder(response).Encode([]cliApp{{Slug: "notes", Name: "Notes", Type: "static", Status: "ready"}})
		default:
			if request.Method != http.MethodPost || request.Header.Get("X-Dropserve-CSRF") != token {
				http.Error(response, "missing token", http.StatusForbidden)
				return
			}
			if request.Header.Get("Origin") == "" {
				http.Error(response, "missing origin", http.StatusForbidden)
				return
			}
			mutations = append(mutations, request.URL.Path)
			response.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	api := &localAPI{baseURL: server.URL, client: server.Client()}
	var applications []cliApp
	if err := api.get(context.Background(), "/_dropserve/api/apps", &applications); err != nil {
		t.Fatalf("get live apps: %v", err)
	}
	if len(applications) != 1 || applications[0].Slug != "notes" {
		t.Fatalf("live apps = %#v", applications)
	}
	for _, path := range []string{
		"/_dropserve/api/apps/notes/restart",
		"/_dropserve/api/sharing/tailscale",
		"/_dropserve/api/sharing/funnel/notes",
		"/_dropserve/api/addons/php",
	} {
		if err := api.post(context.Background(), path, map[string]any{"enabled": true}); err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
	}
	if len(mutations) != 4 || api.token != token {
		t.Fatalf("mutations=%v token=%q", mutations, api.token)
	}
}

func TestWaitForRuntimeInstallPollsUntilTheQueuedChangeFinishes(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_dropserve/api/addons" {
			http.NotFound(response, request)
			return
		}
		calls++
		status := cliAddonStatus{Name: "php", Available: true, Busy: calls == 1, Installed: calls > 1}
		_ = json.NewEncoder(response).Encode([]cliAddonStatus{status})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForRuntimeInstall(ctx, &localAPI{baseURL: server.URL, client: server.Client()}, "php"); err != nil {
		t.Fatalf("wait for queued PHP install: %v", err)
	}
	if calls < 2 {
		t.Fatalf("add-on status calls = %d, want at least 2", calls)
	}
}

func TestWaitForRuntimeInstallReturnsTheManagerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode([]cliAddonStatus{{
			Name: "php", Available: true, Message: "SHA-256 verification failed",
		}})
	}))
	defer server.Close()
	err := waitForRuntimeInstall(context.Background(), &localAPI{baseURL: server.URL, client: server.Client()}, "php")
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("terminal runtime error = %v", err)
	}
}

func TestConfigPathAndValidateCommandsUseTheSelectedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := configCommand([]string{"path"}, &stdout, &stderr, path); code != 0 || strings.TrimSpace(stdout.String()) != path {
		t.Fatalf("config path code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := configCommand([]string{"validate"}, &stdout, &stderr, path); code != 0 || !strings.Contains(stdout.String(), "Configuration is valid") {
		t.Fatalf("config validate code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(path, []byte("[broken"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := configCommand([]string{"validate"}, &stdout, &stderr, path); code != 1 || !strings.Contains(stderr.String(), "invalid") {
		t.Fatalf("invalid config code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
