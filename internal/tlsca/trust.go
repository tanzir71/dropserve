package tlsca

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/smallstep/truststore"
)

// IsTrusted asks the operating system whether the exact persisted Dropserve
// root currently chains through its system trust roots. It performs no write.
func IsTrusted(rootPath string) (bool, error) {
	content, err := os.ReadFile(rootPath) // #nosec G304 -- caller supplies Dropserve's fixed root path.
	if err != nil {
		return false, fmt.Errorf("read Dropserve local root: %w", err)
	}
	block, _ := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" {
		return false, errors.New("dropserve local root is not a PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("parse Dropserve local root: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return false, fmt.Errorf("read system trust roots: %w", err)
	}
	_, err = certificate.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: certificate.NotBefore.Add(time.Second),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

// TrustStore is the only boundary allowed to change a machine trust store.
type TrustStore interface {
	InstallFile(string) error
	UninstallFile(string) error
}

// TrustController exposes explicit, reversible trust actions for one root.
type TrustController struct {
	rootPath string
	store    TrustStore
}

// NewTrustController creates an inert controller. Construction performs no
// trust action.
func NewTrustController(rootPath string, store TrustStore) *TrustController {
	if store == nil {
		store = systemTrustStore{}
	}
	return &TrustController{rootPath: rootPath, store: store}
}

// Install explicitly adds the local root to this machine's trust store.
func (controller *TrustController) Install() error {
	if err := controller.store.InstallFile(controller.rootPath); err != nil {
		return fmt.Errorf("install Dropserve local trust: %w", err)
	}
	return nil
}

// Uninstall explicitly removes the local root from this machine's trust store.
func (controller *TrustController) Uninstall() error {
	if err := controller.store.UninstallFile(controller.rootPath); err != nil {
		return fmt.Errorf("remove Dropserve local trust: %w", err)
	}
	return nil
}

type systemTrustStore struct{}

func (systemTrustStore) InstallFile(path string) error {
	return truststore.InstallFile(path)
}

func (systemTrustStore) UninstallFile(path string) error {
	return truststore.UninstallFile(path)
}
