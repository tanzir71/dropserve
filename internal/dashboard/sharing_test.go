package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/discovery"
	"github.com/tanzir71/dropserve/internal/indexer"
)

func TestLoopbackOnlySharingHasNoBrokenEntries(t *testing.T) {
	handler, err := NewWithOptions(nil, Options{
		Discovery: func() discovery.Snapshot { return discovery.Snapshot{} },
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/_dropserve/api/urls", nil)
	if err != nil {
		t.Fatalf("create sharing request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("fetch sharing URLs: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var entries []advertisedURL
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatalf("decode sharing URLs: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "loopback" || entries[0].URL != server.URL+"/" {
		t.Fatalf("loopback-only sharing entries = %#v, want only %s/", entries, server.URL)
	}

	request, err = http.NewRequestWithContext(context.Background(), http.MethodGet, entries[0].URL, nil)
	if err != nil {
		t.Fatalf("create advertised URL request: %v", err)
	}
	advertised, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("fetch advertised loopback URL: %v", err)
	}
	_ = advertised.Body.Close()
	if advertised.StatusCode >= http.StatusBadRequest {
		t.Fatalf("advertised loopback URL returned %d, want < 400", advertised.StatusCode)
	}
}

func TestMDNSBindFailureIsLoggedAndOmittedFromSharingURLs(t *testing.T) {
	bindErr := errors.New("udp 5353 already in use")
	var logs bytes.Buffer
	manager := discovery.NewManager(discovery.ManagerOptions{
		LANIP: netip.MustParseAddr("192.168.68.110"),
		Logf: func(format string, arguments ...any) {
			_, _ = logs.WriteString(strings.TrimSpace(formatMessage(format, arguments...)) + "\n")
		},
		RegisterMDNS: func(discovery.MDNSRegistration) (discovery.MDNSResponder, error) {
			return nil, bindErr
		},
	})
	manager.StartMDNS()
	defer manager.Close()
	if !strings.Contains(logs.String(), "mDNS unavailable") || !strings.Contains(logs.String(), bindErr.Error()) {
		t.Fatalf("mDNS failure log = %q, want context and bind error", logs.String())
	}

	handler, err := NewWithOptions(nil, Options{Discovery: manager.Snapshot})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/_dropserve/api/urls", nil)
	if err != nil {
		t.Fatalf("create sharing request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("fetch sharing URLs: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var entries []advertisedURL
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatalf("decode sharing URLs: %v", err)
	}
	for _, entry := range entries {
		if entry.Kind == "mdns" || strings.Contains(entry.URL, ".local") {
			t.Fatalf("failed mDNS responder leaked a dead URL: %#v", entries)
		}
	}
}

func formatMessage(format string, arguments ...any) string {
	return fmt.Sprintf(format, arguments...)
}

func TestFunnelEnableWithoutMatchingSlugIsRefusedBeforeExecution(t *testing.T) {
	executions := 0
	funnel, err := discovery.NewFunnelManager(discovery.FunnelOptions{
		Execute: func(context.Context, discovery.FunnelAction) error {
			executions++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("create Funnel manager: %v", err)
	}
	httpHandler, err := NewWithOptions([]indexer.Entry{{Slug: "field-notes", Name: "Field Notes"}}, Options{Funnel: funnel})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/_dropserve/api/sharing/funnel/field-notes",
		strings.NewReader(`{}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Dropserve-CSRF", dashboard.csrfToken)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing-confirmation response = %d, want 400; body=%q", response.Code, response.Body.String())
	}
	if executions != 0 {
		t.Fatalf("Funnel executor ran %d times, want 0", executions)
	}
}

func TestActiveFunnelProducesNonDismissiblePublicSharingWarning(t *testing.T) {
	now := time.Date(2026, time.August, 28, 2, 0, 0, 0, time.UTC)
	funnel, err := discovery.NewFunnelManager(discovery.FunnelOptions{
		Clock: func() time.Time { return now },
		Execute: func(context.Context, discovery.FunnelAction) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("create Funnel manager: %v", err)
	}
	if err := funnel.Enable(context.Background(), "field-notes", "field-notes"); err != nil {
		t.Fatalf("enable Funnel: %v", err)
	}
	httpHandler, err := NewWithOptions([]indexer.Entry{{Slug: "field-notes", Name: "Field Notes"}}, Options{Funnel: funnel})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/status", nil))
	var status struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !strings.Contains(strings.Join(status.Warnings, "\n"), "public_sharing_active") {
		t.Fatalf("status warnings = %#v, want public_sharing_active", status.Warnings)
	}
	markup := string(dashboard.index)
	start := strings.Index(markup, `id="public-sharing-warning"`)
	if start < 0 {
		t.Fatal("dashboard has no dedicated public-sharing warning")
	}
	end := strings.Index(markup[start:], "</aside>")
	if end < 0 || strings.Contains(markup[start:start+end], "<button") {
		t.Fatal("public-sharing warning must be a permanent banner with no dismiss button")
	}
}
