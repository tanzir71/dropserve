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
	"path/filepath"
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
	discoveryManager := discovery.NewManager(discovery.ManagerOptions{})
	defer discoveryManager.Close()
	discoveryManager.UpdateTailscale(discovery.TailscaleStatus{
		BackendState: "Running",
		Host:         "darkhorse.example-tailnet.ts.net",
	})
	httpHandler, err := NewWithOptions([]indexer.Entry{{Slug: "field-notes", Name: "Field Notes"}}, Options{
		Discovery: discoveryManager.Snapshot,
		Funnel:    funnel,
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/status", nil))
	var status struct {
		Warnings []string `json:"warnings"`
		Sharing  struct {
			Public []struct {
				Slug      string    `json:"slug"`
				URL       string    `json:"url"`
				ExpiresAt time.Time `json:"expires_at"`
			} `json:"public"`
		} `json:"sharing"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !strings.Contains(strings.Join(status.Warnings, "\n"), "public_sharing_active") {
		t.Fatalf("status warnings = %#v, want public_sharing_active", status.Warnings)
	}
	if len(status.Sharing.Public) != 1 ||
		status.Sharing.Public[0].Slug != "field-notes" ||
		status.Sharing.Public[0].URL != "https://darkhorse.example-tailnet.ts.net/field-notes/" ||
		!status.Sharing.Public[0].ExpiresAt.Equal(now.Add(8*time.Hour)) {
		t.Fatalf("public sharing status = %#v", status.Sharing.Public)
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

func TestLANIPChangeNoticePersistsUntilDismissed(t *testing.T) {
	noticePath := filepath.Join(t.TempDir(), "network.json")
	oldIP := netip.MustParseAddr("192.168.1.10")
	newIP := netip.MustParseAddr("192.168.1.77")
	manager := discovery.NewManager(discovery.ManagerOptions{LANIP: oldIP, NoticePath: noticePath})
	manager.UpdateLANIP(newIP)
	defer manager.Close()

	httpHandler, err := NewWithOptions(nil, Options{
		Discovery:            manager.Snapshot,
		DismissNetworkChange: manager.DismissNetworkChange,
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	statusResponse := httptest.NewRecorder()
	dashboard.ServeHTTP(statusResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/status", nil))
	var status struct {
		Network struct {
			Change *struct {
				OldLANIP string `json:"old_lan_ip"`
				NewLANIP string `json:"new_lan_ip"`
			} `json:"change"`
		} `json:"network"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Network.Change == nil || status.Network.Change.OldLANIP != oldIP.String() || status.Network.Change.NewLANIP != newIP.String() {
		t.Fatalf("network change = %#v, want %s -> %s", status.Network.Change, oldIP, newIP)
	}

	page := httptest.NewRecorder()
	dashboard.ServeHTTP(page, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/help/dhcp-reservation", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "DHCP reservation") {
		t.Fatalf("DHCP explainer = %d %q", page.Code, page.Body.String())
	}
	if !strings.Contains(string(dashboard.index), `id="address-change-warning"`) || !strings.Contains(string(dashboard.index), `href="/_dropserve/help/dhcp-reservation"`) {
		t.Fatal("dashboard does not wire the persistent address-change notice to the DHCP explainer")
	}

	persisted := discovery.NewManager(discovery.ManagerOptions{LANIP: newIP, NoticePath: noticePath})
	defer persisted.Close()
	if persisted.Snapshot().LANChange == nil {
		t.Fatal("network change notice did not survive manager reload")
	}
	dismissRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/_dropserve/api/network-change/dismiss", nil)
	dismissRequest.Header.Set("X-Dropserve-CSRF", dashboard.csrfToken)
	dismissResponse := httptest.NewRecorder()
	dashboard.ServeHTTP(dismissResponse, dismissRequest)
	if dismissResponse.Code != http.StatusNoContent {
		t.Fatalf("dismiss response = %d, want 204; body=%q", dismissResponse.Code, dismissResponse.Body.String())
	}
	reloaded := discovery.NewManager(discovery.ManagerOptions{LANIP: newIP, NoticePath: noticePath})
	defer reloaded.Close()
	if reloaded.Snapshot().LANChange != nil {
		t.Fatal("dismissed network change notice returned after reload")
	}
}

func TestTailscaleServeToggleRequiresCSRFAndPublishesHTTPS(t *testing.T) {
	manager := discovery.NewManager(discovery.ManagerOptions{LANIP: netip.MustParseAddr("192.168.1.10")})
	defer manager.Close()
	manager.UpdateTailscale(discovery.TailscaleStatus{
		BackendState: "Running",
		Host:         "darkhorse.example-tailnet.ts.net",
	})
	var transitions []bool
	httpHandler, err := NewWithOptions(nil, Options{
		Discovery: manager.Snapshot,
		SetTailscaleServe: func(_ context.Context, enabled bool) error {
			transitions = append(transitions, enabled)
			return manager.SetTailscaleServeEnabled(enabled)
		},
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)

	unauthorised := httptest.NewRecorder()
	dashboard.ServeHTTP(unauthorised, httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/_dropserve/api/sharing/tailscale",
		strings.NewReader(`{"enabled":true}`),
	))
	if unauthorised.Code != http.StatusForbidden || len(transitions) != 0 {
		t.Fatalf("unauthorised Serve toggle = %d, transitions=%v; want 403 and none", unauthorised.Code, transitions)
	}

	toggleTailscaleServe(t, dashboard, true)
	urlsResponse := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/urls", nil)
	request.Host = "192.168.1.10:8000"
	dashboard.ServeHTTP(urlsResponse, request)
	var urls []advertisedURL
	if err := json.NewDecoder(urlsResponse.Body).Decode(&urls); err != nil {
		t.Fatalf("decode HTTPS sharing URLs: %v", err)
	}
	foundHTTPS := false
	for _, entry := range urls {
		if entry.Kind == "tailscale" && entry.URL == "https://darkhorse.example-tailnet.ts.net/" {
			foundHTTPS = true
		}
	}
	if !foundHTTPS {
		t.Fatalf("sharing URLs = %#v, want verified tailnet HTTPS root", urls)
	}

	toggleTailscaleServe(t, dashboard, false)
	if len(transitions) != 2 || !transitions[0] || transitions[1] {
		t.Fatalf("Serve transitions = %v, want enable then disable", transitions)
	}
}

func toggleTailscaleServe(t *testing.T, dashboard *handler, enabled bool) {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/_dropserve/api/sharing/tailscale",
		strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, enabled)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Dropserve-CSRF", dashboard.csrfToken)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("Serve enabled=%t response = %d, want 204; body=%q", enabled, response.Code, response.Body.String())
	}
}
