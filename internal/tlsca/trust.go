package tlsca

import (
	"fmt"

	"github.com/smallstep/truststore"
)

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
