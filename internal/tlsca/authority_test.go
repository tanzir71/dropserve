package tlsca

import (
	"crypto/x509"
	"encoding/pem"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGeneratedLeafValidatesForEveryLocalAddress(t *testing.T) {
	now := time.Date(2026, time.August, 28, 2, 0, 0, 0, time.UTC)
	authority, err := New(Options{
		Directory: filepath.Join(t.TempDir(), "ca"),
		Hostname:  "darkhorse",
		Addresses: []netip.Addr{netip.MustParseAddr("192.168.50.24")},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	root := readCertificate(t, authority.RootCertificatePath())
	leaf := readCertificate(t, authority.LeafCertificatePath())
	roots := x509.NewCertPool()
	roots.AddCert(root)
	for _, name := range []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"darkhorse",
		"darkhorse.local",
		"dropserve.local",
		"192.168.50.24",
	} {
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:       roots,
			DNSName:     name,
			CurrentTime: now,
		}); err != nil {
			t.Errorf("leaf does not validate for %s: %v", name, err)
		}
	}
	if leaf.NotBefore.After(now.Add(-time.Hour)) {
		t.Fatalf("leaf NotBefore = %s, want at least one hour before %s", leaf.NotBefore, now)
	}
	if _, err := authority.TLSCertificate(); err != nil {
		t.Fatalf("load leaf key pair: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(authority.RootKeyPath())
		if err != nil {
			t.Fatalf("stat root key: %v", err)
		}
		if permission := info.Mode().Perm(); permission != 0o600 {
			t.Fatalf("root key mode = %o, want 600", permission)
		}
	}
}

func TestLANAddressChangeSupersedesTheOldLeaf(t *testing.T) {
	now := time.Date(2026, time.August, 28, 3, 0, 0, 0, time.UTC)
	authority, err := New(Options{
		Directory: filepath.Join(t.TempDir(), "ca"),
		Hostname:  "darkhorse",
		Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.10")},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	oldLeaf := readCertificate(t, authority.LeafCertificatePath())
	changed, err := authority.UpdateAddresses([]netip.Addr{netip.MustParseAddr("192.168.1.77")})
	if err != nil {
		t.Fatalf("update addresses: %v", err)
	}
	if !changed {
		t.Fatal("address change did not report a new leaf")
	}
	newLeaf := readCertificate(t, authority.LeafCertificatePath())
	if oldLeaf.SerialNumber.Cmp(newLeaf.SerialNumber) == 0 {
		t.Fatal("address change reused the old leaf certificate")
	}
	roots := x509.NewCertPool()
	roots.AddCert(readCertificate(t, authority.RootCertificatePath()))
	if _, err := newLeaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "192.168.1.77", CurrentTime: now}); err != nil {
		t.Fatalf("new leaf does not validate for new LAN address: %v", err)
	}
	if _, err := newLeaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "192.168.1.10", CurrentTime: now}); err == nil {
		t.Fatal("new leaf still validates for superseded LAN address")
	}
	changed, err = authority.UpdateAddresses([]netip.Addr{netip.MustParseAddr("192.168.1.77")})
	if err != nil || changed {
		t.Fatalf("unchanged address update = %v, %v; want false, nil", changed, err)
	}
}

func TestFreshLeafAcceptsThirtyMinuteClockSkew(t *testing.T) {
	now := time.Date(2026, time.August, 28, 4, 0, 0, 0, time.UTC)
	authority, err := New(Options{
		Directory: filepath.Join(t.TempDir(), "ca"),
		Hostname:  "darkhorse",
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(readCertificate(t, authority.RootCertificatePath()))
	leaf := readCertificate(t, authority.LeafCertificatePath())
	for _, skew := range []time.Duration{-30 * time.Minute, 30 * time.Minute} {
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:       roots,
			DNSName:     "localhost",
			CurrentTime: now.Add(skew),
		}); err != nil {
			t.Errorf("fresh leaf rejected at clock skew %s: %v", skew, err)
		}
	}
}

func readCertificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	// #nosec G304 -- path is returned by the authority created in this test's temporary directory.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read certificate %s: %v", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("%s is not a PEM certificate", path)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate %s: %v", path, err)
	}
	return certificate
}
