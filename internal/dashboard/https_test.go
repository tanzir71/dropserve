package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/discovery"
)

func TestTrustStoreRequiresExplicitConsent(t *testing.T) {
	status := LocalHTTPSStatus{Enabled: false}
	var httpsTransitions []bool
	var trustTransitions []bool
	httpHandler, err := NewWithOptions(nil, Options{
		LocalHTTPSStatus: func() LocalHTTPSStatus { return status },
		SetLocalHTTPS: func(_ context.Context, enabled bool) error {
			httpsTransitions = append(httpsTransitions, enabled)
			status.Enabled = enabled
			status.Port = 443
			return nil
		},
		SetLocalTrust: func(installed bool) error {
			trustTransitions = append(trustTransitions, installed)
			status.TrustInstalled = installed
			return nil
		},
		RootCertificate: func() ([]byte, error) { return []byte("root certificate"), nil },
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	dashboard := httpHandler.(*handler)
	statusResponse := httptest.NewRecorder()
	dashboard.ServeHTTP(statusResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/status", nil))
	if len(httpsTransitions) != 0 || len(trustTransitions) != 0 {
		t.Fatalf("status/startup changed HTTPS or trust: https=%v trust=%v", httpsTransitions, trustTransitions)
	}

	unauthorised := httptest.NewRecorder()
	dashboard.ServeHTTP(unauthorised, httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/_dropserve/api/trust",
		strings.NewReader(`{"installed":true}`),
	))
	if unauthorised.Code != http.StatusForbidden || len(trustTransitions) != 0 {
		t.Fatalf("unauthorised trust response = %d, calls=%v; want 403 and none", unauthorised.Code, trustTransitions)
	}

	postHTTPSControl(t, dashboard, "/_dropserve/api/https", `{"enabled":true}`)
	postHTTPSControl(t, dashboard, "/_dropserve/api/trust", `{"installed":true}`)
	postHTTPSControl(t, dashboard, "/_dropserve/api/trust", `{"installed":false}`)
	postHTTPSControl(t, dashboard, "/_dropserve/api/https", `{"enabled":false}`)
	if len(httpsTransitions) != 2 || !httpsTransitions[0] || httpsTransitions[1] {
		t.Fatalf("HTTPS transitions = %v, want enable then disable", httpsTransitions)
	}
	if len(trustTransitions) != 2 || !trustTransitions[0] || trustTransitions[1] {
		t.Fatalf("trust transitions = %v, want install then uninstall", trustTransitions)
	}

	rootResponse := httptest.NewRecorder()
	dashboard.ServeHTTP(rootResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/https/root.pem", nil))
	if rootResponse.Code != http.StatusOK || rootResponse.Body.String() != "root certificate" {
		t.Fatalf("root download = %d %q", rootResponse.Code, rootResponse.Body.String())
	}

	finalStatus := httptest.NewRecorder()
	dashboard.ServeHTTP(finalStatus, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/status", nil))
	var payload struct {
		HTTPS LocalHTTPSStatus `json:"https"`
	}
	if err := json.NewDecoder(finalStatus.Body).Decode(&payload); err != nil {
		t.Fatalf("decode final status: %v", err)
	}
	if payload.HTTPS.Enabled || payload.HTTPS.TrustInstalled {
		t.Fatalf("final HTTPS status = %#v, want disabled and untrusted", payload.HTTPS)
	}
}

func TestEnabledLocalHTTPSIsAdvertisedAlongsideHTTP(t *testing.T) {
	httpHandler, err := NewWithOptions(nil, Options{
		Discovery: func() discovery.Snapshot {
			return discovery.Snapshot{
				LANIP:        netip.MustParseAddr("192.168.68.110"),
				MDNSHostname: "darkhorse.local",
			}
		},
		LocalHTTPSStatus: func() LocalHTTPSStatus {
			return LocalHTTPSStatus{Enabled: true, Port: 8443, RootAvailable: true}
		},
	})
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/urls", nil)
	request.Host = "192.168.68.110:8000"
	response := httptest.NewRecorder()
	httpHandler.ServeHTTP(response, request)
	var urls []advertisedURL
	if err := json.NewDecoder(response.Body).Decode(&urls); err != nil {
		t.Fatalf("decode advertised URLs: %v", err)
	}
	want := map[string]string{
		"lan":        "http://192.168.68.110:8000/",
		"mdns":       "http://darkhorse.local:8000/",
		"https-lan":  "https://192.168.68.110:8443/",
		"https-mdns": "https://darkhorse.local:8443/",
	}
	for _, entry := range urls {
		if expected, found := want[entry.Kind]; found && entry.URL == expected {
			delete(want, entry.Kind)
		}
	}
	if len(want) != 0 {
		t.Fatalf("advertised URLs = %#v; missing %#v", urls, want)
	}
	statusResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(statusResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_dropserve/api/status", nil))
	var status struct {
		Ports struct {
			HTTP  int `json:"http"`
			HTTPS int `json:"https"`
		} `json:"ports"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatalf("decode status ports: %v", err)
	}
	if status.Ports.HTTPS != 8443 {
		t.Fatalf("status HTTPS port = %d, want 8443", status.Ports.HTTPS)
	}
}

func postHTTPSControl(t *testing.T, dashboard *handler, path, body string) {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	authorizeTestMutation(request, dashboard.csrfToken)
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("POST %s = %d, want 204; body=%q", path, response.Code, response.Body.String())
	}
}
