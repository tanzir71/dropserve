package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/tlsca"
)

func TestHTTPRemainsAvailableWhenHTTPSIsEnabled(t *testing.T) {
	handler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "same live app")
	})
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	authority, err := tlsca.New(tlsca.Options{
		Directory: filepath.Join(t.TempDir(), "ca"),
		Hostname:  "darkhorse",
		Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.10")},
	})
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open HTTPS listener: %v", err)
	}
	httpsRuntime := NewHTTPSRuntime(handler, authority.TLSCertificate)
	httpsRuntime.Start(listener)
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpsRuntime.Shutdown(shutdownContext)
	}()

	roots := x509.NewCertPool()
	roots.AddCert(readHTTPSRoot(t, authority.RootCertificatePath()))
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	httpsURL := "https://" + httpsRuntime.Address() + "/"
	for _, target := range []string{httpServer.URL + "/", httpsURL} {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("create GET %s: %v", target, err)
		}
		response, err := client.Do(request) // #nosec G107 -- URLs are loopback test servers created above.
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || string(body) != "same live app" {
			t.Fatalf("GET %s = %d %q, read=%v", target, response.StatusCode, body, readErr)
		}
		if response.Request.URL.Scheme != target[:len(response.Request.URL.Scheme)] {
			t.Fatalf("GET %s redirected to %s", target, response.Request.URL)
		}
	}

	first := fetchTLSSerial(t, client, httpsURL)
	if _, err := authority.UpdateAddresses([]netip.Addr{netip.MustParseAddr("192.168.1.77")}); err != nil {
		t.Fatalf("reissue leaf: %v", err)
	}
	transport.CloseIdleConnections()
	second := fetchTLSSerial(t, client, httpsURL)
	if first == second {
		t.Fatal("new TLS handshake kept serving the superseded leaf")
	}
}

func fetchTLSSerial(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("create TLS request: %v", err)
	}
	response, err := client.Do(request) // #nosec G107 -- target is the loopback test runtime.
	if err != nil {
		t.Fatalf("fetch TLS certificate: %v", err)
	}
	_ = response.Body.Close()
	if response.TLS == nil || len(response.TLS.PeerCertificates) == 0 {
		t.Fatal("HTTPS response has no peer certificate")
	}
	return response.TLS.PeerCertificates[0].SerialNumber.String()
}

func readHTTPSRoot(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	// #nosec G304 -- path is returned by the authority created in this test's temporary directory.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("root is not PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	return certificate
}
