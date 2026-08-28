// Package tlsca owns Dropserve's opt-in local certificate authority and leaf
// certificates. It never changes an operating-system trust store.
package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	certificateBackdate             = 2 * time.Hour
	certificateFilesystemRetryDelay = 10 * time.Millisecond
	certificateFilesystemRetryLimit = 100 * time.Millisecond
)

// Options describes the names and addresses covered by a local leaf.
type Options struct {
	Directory string
	Hostname  string
	Addresses []netip.Addr
	Clock     func() time.Time
}

// Authority owns one persisted root and its current server leaf.
type Authority struct {
	mu       sync.RWMutex
	options  Options
	rootCert *x509.Certificate
	rootKey  *ecdsa.PrivateKey
}

// New loads or creates the local root and issues a leaf for the current host.
func New(options Options) (*Authority, error) {
	if strings.TrimSpace(options.Directory) == "" {
		return nil, errors.New("CA directory is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if strings.TrimSpace(options.Hostname) == "" {
		options.Hostname, _ = os.Hostname()
	}
	options.Addresses = normalizeAddresses(options.Addresses)
	if err := os.MkdirAll(options.Directory, 0o700); err != nil {
		return nil, fmt.Errorf("create CA directory: %w", err)
	}
	rootCert, rootKey, err := loadOrCreateRoot(options)
	if err != nil {
		return nil, err
	}
	authority := &Authority{options: options, rootCert: rootCert, rootKey: rootKey}
	if err := authority.issueLeaf(options.Addresses); err != nil {
		return nil, err
	}
	return authority, nil
}

// RootCertificatePath is the public certificate users may explicitly trust.
func (authority *Authority) RootCertificatePath() string {
	return filepath.Join(authority.options.Directory, "root.pem")
}

// RootKeyPath is the private signing key and must never leave this machine.
func (authority *Authority) RootKeyPath() string {
	return filepath.Join(authority.options.Directory, "root-key.pem")
}

// LeafCertificatePath is the current server certificate chain.
func (authority *Authority) LeafCertificatePath() string {
	return filepath.Join(authority.options.Directory, "leaf.pem")
}

// LeafKeyPath is the current server private key.
func (authority *Authority) LeafKeyPath() string {
	return filepath.Join(authority.options.Directory, "leaf-key.pem")
}

// TLSCertificate loads the current leaf key pair for a TLS listener.
func (authority *Authority) TLSCertificate() (tls.Certificate, error) {
	authority.mu.RLock()
	defer authority.mu.RUnlock()
	certificate, err := tls.LoadX509KeyPair(authority.LeafCertificatePath(), authority.LeafKeyPath())
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load Dropserve leaf certificate: %w", err)
	}
	return certificate, nil
}

// UpdateAddresses issues and atomically publishes a new leaf when the verified
// LAN address set changes. Superseded addresses are deliberately omitted.
func (authority *Authority) UpdateAddresses(addresses []netip.Addr) (bool, error) {
	normalized := normalizeAddresses(addresses)
	authority.mu.RLock()
	unchanged := sameAddresses(authority.options.Addresses, normalized)
	authority.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	if err := authority.issueLeaf(normalized); err != nil {
		return false, err
	}
	authority.mu.Lock()
	authority.options.Addresses = normalized
	authority.mu.Unlock()
	return true, nil
}

func loadOrCreateRoot(options Options) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificatePath := filepath.Join(options.Directory, "root.pem")
	keyPath := filepath.Join(options.Directory, "root-key.pem")
	certificateData, certificateErr := os.ReadFile(certificatePath) // #nosec G304 -- fixed filename under Dropserve's state directory.
	keyData, keyErr := os.ReadFile(keyPath)                         // #nosec G304 -- fixed filename under Dropserve's state directory.
	if certificateErr == nil && keyErr == nil {
		certificate, err := parseCertificatePEM(certificateData)
		if err != nil {
			return nil, nil, fmt.Errorf("parse local root certificate: %w", err)
		}
		key, err := parseECKeyPEM(keyData)
		if err != nil {
			return nil, nil, fmt.Errorf("parse local root key: %w", err)
		}
		return certificate, key, nil
	}
	if (!errors.Is(certificateErr, os.ErrNotExist) && certificateErr != nil) ||
		(!errors.Is(keyErr, os.ErrNotExist) && keyErr != nil) {
		return nil, nil, fmt.Errorf("read local root: %w", errors.Join(certificateErr, keyErr))
	}
	if (certificateErr == nil) != (keyErr == nil) {
		return nil, nil, errors.New("local root certificate and key are incomplete")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate local root key: %w", err)
	}
	now := options.Clock().UTC()
	template := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "Dropserve Local CA", Organization: []string{"Dropserve"}},
		NotBefore:             now.Add(-certificateBackdate),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("issue local root certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("parse generated local root: %w", err)
	}
	encodedKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode local root key: %w", err)
	}
	if err := writePrivatePEM(keyPath, "EC PRIVATE KEY", encodedKey); err != nil {
		return nil, nil, err
	}
	if err := writePEM(certificatePath, 0o644, "CERTIFICATE", der); err != nil {
		return nil, nil, err
	}
	return certificate, key, nil
}

func (authority *Authority) issueLeaf(addresses []netip.Addr) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate leaf key: %w", err)
	}
	hostname := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(authority.options.Hostname)), ".")
	dnsNames := uniqueStrings([]string{"localhost", hostname, hostname + ".local", "dropserve.local"})
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	for _, address := range addresses {
		if address.IsValid() && !address.IsUnspecified() {
			ipAddresses = append(ipAddresses, net.IP(address.Unmap().AsSlice()))
		}
	}
	now := authority.options.Clock().UTC()
	template := &x509.Certificate{
		SerialNumber: randomSerial(),
		Subject:      pkix.Name{CommonName: "Dropserve on " + hostname, Organization: []string{"Dropserve"}},
		NotBefore:    now.Add(-certificateBackdate),
		NotAfter:     now.AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  uniqueIPs(ipAddresses),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, authority.rootCert, &key.PublicKey, authority.rootKey)
	if err != nil {
		return fmt.Errorf("issue local leaf certificate: %w", err)
	}
	encodedKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode local leaf key: %w", err)
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if err := writePrivatePEM(authority.LeafKeyPath(), "EC PRIVATE KEY", encodedKey); err != nil {
		return err
	}
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: authority.rootCert.Raw})...)
	if err := writeBytes(authority.LeafCertificatePath(), 0o644, chain); err != nil {
		return err
	}
	return nil
}

func parseCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("missing CERTIFICATE PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseECKeyPEM(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, errors.New("missing EC PRIVATE KEY PEM block")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func writePrivatePEM(path, blockType string, der []byte) error {
	return writePEM(path, 0o600, blockType, der)
}

func writePEM(path string, mode os.FileMode, blockType string, der []byte) error {
	return writeBytes(path, mode, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
}

func writeBytes(path string, mode os.FileMode, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "certificate-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary certificate file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect certificate file: %w", err)
	}
	private := mode.Perm() == 0o600
	if private {
		if err := protectPrivateFile(temporaryPath); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write certificate file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync certificate file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close certificate file: %w", err)
	}
	backup := path + ".bak"
	if err := retryCertificateFilesystem(func() error { return os.Remove(backup) }); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale certificate backup: %w", err)
	}
	if err := retryCertificateFilesystem(func() error { return os.Rename(path, backup) }); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("back up certificate file: %w", err)
	}
	if err := retryCertificateFilesystem(func() error { return os.Rename(temporaryPath, path) }); err != nil {
		_ = retryCertificateFilesystem(func() error { return os.Rename(backup, path) })
		return fmt.Errorf("replace certificate file: %w", err)
	}
	_ = retryCertificateFilesystem(func() error { return os.Remove(backup) })
	if private {
		if err := protectPrivateFile(path); err != nil {
			return err
		}
	}
	return nil
}

func retryCertificateFilesystem(operation func() error) error {
	deadline := time.Now().Add(certificateFilesystemRetryLimit)
	for {
		err := operation()
		if err == nil {
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(certificateFilesystemRetryDelay)
	}
}

func randomSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil || serial.Sign() == 0 {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueIPs(values []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(values))
	result := make([]net.IP, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		key := value.String()
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeAddresses(addresses []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(addresses))
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() {
			continue
		}
		if _, found := seen[address]; found {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address)
	}
	sort.Slice(result, func(first, second int) bool {
		return result[first].Compare(result[second]) < 0
	})
	return result
}

func sameAddresses(first, second []netip.Addr) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
