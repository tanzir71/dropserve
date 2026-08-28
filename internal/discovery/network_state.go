package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type networkDiskState struct {
	LastLANIP string     `json:"last_lan_ip,omitempty"`
	Change    *LANChange `json:"change,omitempty"`
}

func (manager *Manager) initializeNetworkState() {
	if manager.noticePath == "" {
		return
	}
	state, err := loadNetworkState(manager.noticePath)
	if err != nil {
		manager.logf("load LAN address history: %v", err)
		return
	}
	current := ""
	if manager.snapshot.LANIP.IsValid() {
		current = manager.snapshot.LANIP.String()
	}
	manager.lastLANIP = state.LastLANIP
	manager.snapshot.LANChange = state.Change
	if current != "" {
		if manager.lastLANIP != "" && manager.lastLANIP != current {
			manager.snapshot.LANChange = &LANChange{OldLANIP: manager.lastLANIP, NewLANIP: current}
		}
		manager.lastLANIP = current
	}
	if err := manager.saveNetworkStateLocked(); err != nil {
		manager.logf("persist LAN address history: %v", err)
	}
}

func (manager *Manager) saveNetworkStateLocked() error {
	if manager.noticePath == "" {
		return nil
	}
	return saveNetworkState(manager.noticePath, networkDiskState{
		LastLANIP: manager.lastLANIP,
		Change:    manager.snapshot.LANChange,
	})
}

func loadNetworkState(path string) (networkDiskState, error) {
	// #nosec G304 -- path is the configured Dropserve machine-state path.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return networkDiskState{}, nil
	}
	if err != nil {
		return networkDiskState{}, fmt.Errorf("read LAN address history: %w", err)
	}
	var state networkDiskState
	if err := json.Unmarshal(data, &state); err != nil {
		return networkDiskState{}, fmt.Errorf("parse LAN address history: %w", err)
	}
	return state, nil
}

func saveNetworkState(path string, state networkDiskState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode LAN address history: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create LAN address state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "network-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary LAN address state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary LAN address state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary LAN address state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary LAN address state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary LAN address state: %w", err)
	}
	backup := path + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("back up LAN address state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Rename(backup, path)
		return fmt.Errorf("replace LAN address state: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}
