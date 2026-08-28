package discovery

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/tlsca"
)

func TestLANChangeReissuesLeafWithinOneMonitorInterval(t *testing.T) {
	oldAddress := netip.MustParseAddr("192.168.1.10")
	newAddress := netip.MustParseAddr("192.168.1.77")
	authority, err := tlsca.New(tlsca.Options{
		Directory: filepath.Join(t.TempDir(), "ca"),
		Hostname:  "darkhorse",
		Addresses: []netip.Addr{oldAddress},
	})
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	manager := NewManager(ManagerOptions{LANIP: oldAddress})
	defer manager.Close()
	events := make(chan struct{}, 1)
	const interval = 250 * time.Millisecond
	monitor := NewMonitor(MonitorOptions{
		Interval:   interval,
		Events:     events,
		Manager:    manager,
		ProbeLANIP: func() (netip.Addr, error) { return newAddress, nil },
		UpdateTLSAddresses: func(addresses []netip.Addr) error {
			_, updateErr := authority.UpdateAddresses(addresses)
			return updateErr
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Run(ctx)
	started := time.Now()
	events <- struct{}{}
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	deadline := time.NewTimer(interval)
	defer deadline.Stop()
	var lastReadError error
	for {
		leaf, readErr := readMonitorLeaf(authority.LeafCertificatePath())
		lastReadError = readErr
		if readErr == nil && hasCertificateIP(leaf, newAddress) {
			if hasCertificateIP(leaf, oldAddress) {
				t.Fatal("reissued leaf retained the superseded LAN address")
			}
			break
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatalf("leaf was not reissued within %s (elapsed %s, last read error: %v)", interval, time.Since(started), lastReadError)
		}
	}
}

func readMonitorLeaf(path string) (*x509.Certificate, error) {
	// #nosec G304 -- path is returned by the authority created in this test's temporary directory.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read leaf: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("leaf is not PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse leaf: %w", err)
	}
	return certificate, nil
}

func hasCertificateIP(certificate *x509.Certificate, address netip.Addr) bool {
	for _, candidate := range certificate.IPAddresses {
		parsed, ok := netip.AddrFromSlice(candidate)
		if ok && parsed.Unmap() == address.Unmap() {
			return true
		}
	}
	return false
}
