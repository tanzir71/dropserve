package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/discovery"
	"github.com/tanzir71/dropserve/internal/indexer"
)

func TestNetworkChangeRecoversClosedListenerAndRefreshesAddresses(t *testing.T) {
	manager := discovery.NewManager(discovery.ManagerOptions{LANIP: netip.MustParseAddr("192.168.1.10")})
	defer manager.Close()
	dashboardHandler, err := dashboard.NewWithOptions(
		[]indexer.Entry{{Slug: "field-notes", Name: "Field Notes"}},
		dashboard.Options{Discovery: manager.Snapshot},
	)
	if err != nil {
		t.Fatalf("create dashboard: %v", err)
	}
	var appRequests atomic.Int32
	mux := http.NewServeMux()
	mux.Handle("/", dashboardHandler)
	mux.HandleFunc("/field-notes/", func(response http.ResponseWriter, _ *http.Request) {
		appRequests.Add(1)
		_, _ = io.WriteString(response, "field notes survived")
	})

	initial, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open initial listener: %v", err)
	}
	runtime := NewListenerRuntime(mux)
	runtime.Start(initial)
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = runtime.Shutdown(shutdownContext)
	}()
	oldAddress := runtime.Address()
	if err := runtime.CloseActiveListener(); err != nil {
		t.Fatalf("close listener underneath server: %v", err)
	}
	waitForCondition(t, time.Second, func() bool { return !runtime.Healthy() }, "listener to become unhealthy")

	changedIP := netip.MustParseAddr("192.168.1.77")
	events := make(chan struct{}, 1)
	const interval = 250 * time.Millisecond
	monitor := discovery.NewMonitor(discovery.MonitorOptions{
		Interval: interval,
		Events:   events,
		Manager:  manager,
		ProbeLANIP: func() (netip.Addr, error) {
			return changedIP, nil
		},
		ListenerHealthy: runtime.Healthy,
		RecoverListener: func(recoveryContext context.Context) error {
			listener, listenErr := (&net.ListenConfig{}).Listen(recoveryContext, "tcp4", "127.0.0.1:0")
			if listenErr != nil {
				return listenErr
			}
			runtime.Start(listener)
			return nil
		},
	})
	monitorContext, stopMonitor := context.WithCancel(context.Background())
	defer stopMonitor()
	go monitor.Run(monitorContext)
	started := time.Now()
	events <- struct{}{}
	waitForCondition(t, interval, func() bool {
		return runtime.Healthy() && runtime.Address() != oldAddress && manager.Snapshot().LANIP == changedIP
	}, "network and listener recovery")
	if elapsed := time.Since(started); elapsed > interval {
		t.Fatalf("recovery took %s, want at most one %s monitor interval", elapsed, interval)
	}

	baseURL := "http://" + runtime.Address()
	appResponse := getRecoveryURL(t, baseURL+"/field-notes/")
	appBody, err := io.ReadAll(appResponse.Body)
	_ = appResponse.Body.Close()
	if err != nil || appResponse.StatusCode != http.StatusOK || string(appBody) != "field notes survived" {
		t.Fatalf("app after listener recovery = %d %q, err=%v", appResponse.StatusCode, appBody, err)
	}
	if appRequests.Load() != 1 {
		t.Fatalf("app request count = %d, want 1 after recovery", appRequests.Load())
	}

	statusResponse := getRecoveryURL(t, baseURL+"/_dropserve/api/status")
	var status struct {
		Network struct {
			LANIP string `json:"lan_ip"`
		} `json:"network"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		_ = statusResponse.Body.Close()
		t.Fatalf("decode status: %v", err)
	}
	_ = statusResponse.Body.Close()
	if status.Network.LANIP != changedIP.String() {
		t.Fatalf("status LAN IP = %q, want %s", status.Network.LANIP, changedIP)
	}
	urlsResponse := getRecoveryURL(t, baseURL+"/_dropserve/api/urls")
	var urls []struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(urlsResponse.Body).Decode(&urls); err != nil {
		_ = urlsResponse.Body.Close()
		t.Fatalf("decode URLs: %v", err)
	}
	_ = urlsResponse.Body.Close()
	if len(urls) == 0 || urls[0].Kind != "lan" || !strings.Contains(urls[0].URL, changedIP.String()) {
		t.Fatalf("updated URLs = %#v, want LAN address %s", urls, changedIP)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func getRecoveryURL(t *testing.T, address string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatalf("create request for %s: %v", address, err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("fetch %s: %v", address, err)
	}
	return response
}
