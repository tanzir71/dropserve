package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type localHTTPSRecordingTrustStore struct {
	installed   []string
	uninstalled []string
}

func (store *localHTTPSRecordingTrustStore) InstallFile(path string) error {
	store.installed = append(store.installed, path)
	return nil
}

func (store *localHTTPSRecordingTrustStore) UninstallFile(path string) error {
	store.uninstalled = append(store.uninstalled, path)
	return nil
}

func TestLocalHTTPSControllerChangesListenersAndTrustOnlyOnExplicitActions(t *testing.T) {
	stateDirectory := t.TempDir()
	port := reserveLocalHTTPSTestPort(t)
	store := &localHTTPSRecordingTrustStore{}
	var persisted []int
	controller := newLocalHTTPSController(localHTTPSOptions{
		Bind:           "127.0.0.1",
		PreferredPort:  port,
		StateDirectory: stateDirectory,
		Hostname:       "darkhorse",
		Addresses:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("192.168.68.110")} },
		Handler: func() http.Handler {
			return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(response, "same handler") })
		},
		PersistPort: func(selected int) error {
			persisted = append(persisted, selected)
			return nil
		},
		TrustStore: store,
	})
	if status := controller.Status(); status.Enabled || status.RootAvailable || status.TrustInstalled {
		t.Fatalf("construction status = %#v, want inert", status)
	}
	if len(persisted) != 0 || len(store.installed) != 0 || len(store.uninstalled) != 0 {
		t.Fatalf("construction changed state: ports=%v installs=%v uninstalls=%v", persisted, store.installed, store.uninstalled)
	}

	if err := controller.SetEnabled(context.Background(), true); err != nil {
		t.Fatalf("explicitly enable local HTTPS: %v", err)
	}
	status := controller.Status()
	if !status.Enabled || !status.RootAvailable || status.Port != port || status.TrustInstalled {
		t.Fatalf("enabled status = %#v", status)
	}
	rootPEM, err := controller.RootCertificate()
	if err != nil {
		t.Fatalf("read root certificate: %v", err)
	}
	client := localHTTPSTestClient(t, rootPEM)
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://127.0.0.1:"+strconv.Itoa(port)+"/",
		nil,
	)
	if err != nil {
		t.Fatalf("create local HTTPS request: %v", err)
	}
	response, err := client.Do(request) // #nosec G107 -- test owns this loopback listener.
	if err != nil {
		t.Fatalf("fetch explicitly enabled HTTPS listener: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "same handler" {
		t.Fatalf("HTTPS response = %d %q, err=%v", response.StatusCode, body, err)
	}

	if err := controller.SetTrust(true); err != nil {
		t.Fatalf("explicitly install trust: %v", err)
	}
	if err := controller.SetTrust(false); err != nil {
		t.Fatalf("explicitly uninstall trust: %v", err)
	}
	if err := controller.SetEnabled(context.Background(), false); err != nil {
		t.Fatalf("explicitly disable local HTTPS: %v", err)
	}
	if got := controller.Status(); got.Enabled || got.TrustInstalled || !got.RootAvailable {
		t.Fatalf("disabled status = %#v, want retained downloadable root and no active listener/trust", got)
	}
	rootPath := filepath.Join(stateDirectory, "ca", "root.pem")
	if !reflect.DeepEqual(persisted, []int{port, 0}) ||
		!reflect.DeepEqual(store.installed, []string{rootPath}) ||
		!reflect.DeepEqual(store.uninstalled, []string{rootPath}) {
		t.Fatalf("explicit effects: ports=%v installs=%v uninstalls=%v", persisted, store.installed, store.uninstalled)
	}
}

func TestLocalHTTPSEnableRollsBackWhenPersistenceFails(t *testing.T) {
	port := reserveLocalHTTPSTestPort(t)
	controller := newLocalHTTPSController(localHTTPSOptions{
		Bind:           "127.0.0.1",
		PreferredPort:  port,
		StateDirectory: t.TempDir(),
		Handler:        func() http.Handler { return http.NotFoundHandler() },
		PersistPort:    func(int) error { return errors.New("disk full") },
	})
	if err := controller.SetEnabled(context.Background(), true); err == nil {
		t.Fatal("enable succeeded despite persistence failure")
	}
	if controller.Status().Enabled {
		t.Fatal("persistence failure left HTTPS reported as enabled")
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("failed enable leaked its listener: %v", err)
	}
	_ = listener.Close()
}

func reserveLocalHTTPSTestPort(t *testing.T) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local HTTPS test port: %v", err)
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	_ = listener.Close()
	if err != nil {
		t.Fatalf("read local HTTPS test port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse local HTTPS test port: %v", err)
	}
	return port
}

func localHTTPSTestClient(t *testing.T, rootPEM []byte) *http.Client {
	t.Helper()
	block, _ := pem.Decode(rootPEM)
	if block == nil {
		t.Fatal("decode local HTTPS root PEM")
	}
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse local HTTPS root: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
		Timeout:   5 * time.Second,
	}
}

func TestTrustCommandIsExplicitAndReversible(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	rootPath := filepath.Join(filepath.Dir(statePath), "ca", "root.pem")
	if err := os.MkdirAll(filepath.Dir(rootPath), 0o700); err != nil {
		t.Fatalf("create CA directory: %v", err)
	}
	if err := os.WriteFile(rootPath, []byte("test root"), 0o600); err != nil {
		t.Fatalf("write test root: %v", err)
	}
	store := &localHTTPSRecordingTrustStore{}
	if code := trustCommandWithStore([]string{"--install"}, io.Discard, io.Discard, statePath, store); code != 0 {
		t.Fatalf("trust --install returned %d", code)
	}
	if code := trustCommandWithStore([]string{"--uninstall"}, io.Discard, io.Discard, statePath, store); code != 0 {
		t.Fatalf("trust --uninstall returned %d", code)
	}
	if !reflect.DeepEqual(store.installed, []string{rootPath}) || !reflect.DeepEqual(store.uninstalled, []string{rootPath}) {
		t.Fatalf("trust command effects: installs=%v uninstalls=%v", store.installed, store.uninstalled)
	}
}
