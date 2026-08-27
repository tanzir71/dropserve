// Package state persists Dropserve's machine-managed runtime state as atomic JSON.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Warning is a machine-readable runtime warning with friendly copy.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// State is the small runtime snapshot persisted between starts.
type State struct {
	HTTPPort int       `json:"http_port"`
	Warnings []Warning `json:"warnings,omitempty"`
}

// DefaultPath returns the per-user runtime-state path.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		root, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("find local app-data directory: %w", err)
		}
		return filepath.Join(root, "Dropserve", "state.json"), nil
	}
	if root := os.Getenv("XDG_DATA_HOME"); root != "" {
		return filepath.Join(root, "dropserve", "state.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "dropserve", "state.json"), nil
}

// Load reads a runtime snapshot, returning an empty state when absent.
func Load(path string) (State, error) {
	// #nosec G304 -- path is Dropserve's configured per-user state path, not request data.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state %q: %w", path, err)
	}
	var snapshot State
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	return snapshot, nil
}

// Save atomically replaces the runtime snapshot and keeps one backup.
func Save(path string, snapshot State) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}

	backup := path + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("back up prior state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Rename(backup, path)
		return fmt.Errorf("replace state: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}
