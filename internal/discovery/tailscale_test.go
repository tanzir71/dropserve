package discovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTailscaleStatusFixturesExposeOnlyRunningURL(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		wantURL     string
		wantMessage string
	}{
		{name: "running", fixture: "tailscale-running.json", wantURL: "http://darkhorse.example-tailnet.ts.net/"},
		{name: "stopped", fixture: "tailscale-stopped.json", wantMessage: "Tailscale is stopped"},
		{name: "needs login", fixture: "tailscale-needs-login.json", wantMessage: "Sign in to Tailscale"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", test.fixture))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			status, err := ParseTailscaleStatus(data)
			if err != nil {
				t.Fatalf("parse status: %v", err)
			}
			endpoints := (Snapshot{Tailscale: status}).Endpoints("http", 80)
			var tailscale Endpoint
			for _, endpoint := range endpoints {
				if endpoint.Kind == "tailscale" {
					tailscale = endpoint
				}
			}
			if tailscale.URL != test.wantURL {
				t.Fatalf("Tailscale URL = %q, want %q", tailscale.URL, test.wantURL)
			}
			if !strings.Contains(tailscale.Message, test.wantMessage) {
				t.Fatalf("Tailscale message = %q, want it to contain %q", tailscale.Message, test.wantMessage)
			}
		})
	}
}

func TestMissingTailscaleBinaryIsExplainedWithoutError(t *testing.T) {
	runCalled := false
	status, err := ProbeTailscale(context.Background(), TailscaleProbes{
		GOOS: "windows",
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
		Exists: func(string) bool { return false },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			runCalled = true
			return nil, errors.New("must not run")
		},
	})
	if err != nil {
		t.Fatalf("ProbeTailscale() returned %v, want a normal not-installed state", err)
	}
	if runCalled {
		t.Fatal("ProbeTailscale() ran a command even though no binary was present")
	}
	if status.BackendState != "NotInstalled" || !strings.Contains(status.Message, "not installed") || status.Host != "" {
		t.Fatalf("missing Tailscale status = %#v", status)
	}
	endpoints := (Snapshot{Tailscale: status}).Endpoints("http", 80)
	for _, endpoint := range endpoints {
		if endpoint.Kind == "tailscale" && endpoint.URL != "" {
			t.Fatalf("missing Tailscale advertised a dead URL: %#v", endpoints)
		}
	}
}

func TestTailscaleSharingCommandsAreScopedAndReversible(t *testing.T) {
	type invocation struct {
		path string
		args []string
	}
	var invocations []invocation
	probes := TailscaleProbes{
		GOOS:     "linux",
		LookPath: func(string) (string, error) { return "/usr/bin/tailscale", nil },
		Exists:   func(string) bool { return false },
		Run: func(_ context.Context, path string, arguments ...string) ([]byte, error) {
			invocations = append(invocations, invocation{path: path, args: append([]string{}, arguments...)})
			return []byte("ok"), nil
		},
	}
	executeFunnel := TailscaleFunnelExecutor(8000, probes)
	if err := executeFunnel(context.Background(), FunnelAction{Slug: "field-notes", Enable: true}); err != nil {
		t.Fatalf("enable Funnel: %v", err)
	}
	if err := executeFunnel(context.Background(), FunnelAction{Slug: "field-notes", Enable: false}); err != nil {
		t.Fatalf("disable Funnel: %v", err)
	}
	if err := SetTailscaleServe(context.Background(), 8000, true, probes); err != nil {
		t.Fatalf("enable Serve: %v", err)
	}
	if err := SetTailscaleServe(context.Background(), 8000, false, probes); err != nil {
		t.Fatalf("disable Serve: %v", err)
	}
	want := [][]string{
		{"funnel", "--bg", "--yes", "--https=443", "--set-path=/field-notes", "http://127.0.0.1:8000/field-notes/"},
		{"funnel", "--https=443", "--set-path=/field-notes", "off"},
		{"serve", "--bg", "--yes", "--https=443", "http://127.0.0.1:8000"},
		{"serve", "--https=443", "off"},
	}
	if len(invocations) != len(want) {
		t.Fatalf("Tailscale invocations = %#v, want %d", invocations, len(want))
	}
	for index := range want {
		if invocations[index].path != "/usr/bin/tailscale" || !reflect.DeepEqual(invocations[index].args, want[index]) {
			t.Fatalf("invocation %d = %#v, want path and args %#v", index, invocations[index], want[index])
		}
	}
}
