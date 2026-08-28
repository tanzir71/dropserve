package tlsca

import (
	"path/filepath"
	"testing"
)

type recordingTrustStore struct {
	installed   []string
	uninstalled []string
}

func (store *recordingTrustStore) InstallFile(path string) error {
	store.installed = append(store.installed, path)
	return nil
}

func (store *recordingTrustStore) UninstallFile(path string) error {
	store.uninstalled = append(store.uninstalled, path)
	return nil
}

func TestTrustInstallAndUninstallAreExplicitAndReversible(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root.pem")
	store := &recordingTrustStore{}
	controller := NewTrustController(rootPath, store)
	if len(store.installed) != 0 || len(store.uninstalled) != 0 {
		t.Fatal("constructing the trust controller changed the OS trust store")
	}
	if err := controller.Install(); err != nil {
		t.Fatalf("install trust: %v", err)
	}
	if err := controller.Uninstall(); err != nil {
		t.Fatalf("uninstall trust: %v", err)
	}
	if len(store.installed) != 1 || store.installed[0] != rootPath {
		t.Fatalf("install calls = %v, want only %s", store.installed, rootPath)
	}
	if len(store.uninstalled) != 1 || store.uninstalled[0] != rootPath {
		t.Fatalf("uninstall calls = %v, want only %s", store.uninstalled, rootPath)
	}
}
