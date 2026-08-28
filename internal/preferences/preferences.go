// Package preferences persists dashboard-only app presentation state without
// writing into user app folders.
package preferences

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// App is one app's durable dashboard presentation state.
type App struct {
	Pinned   *bool     `json:"pinned,omitempty"`
	Hidden   *bool     `json:"hidden,omitempty"`
	LastUsed time.Time `json:"last_used,omitempty"`
}

type diskState struct {
	Apps map[string]App `json:"apps"`
}

// Store owns one atomic per-user preferences file.
type Store struct {
	mu   sync.Mutex
	path string
	apps map[string]App
	now  func() time.Time
}

// Open loads an existing file or returns an empty store when it is absent.
func Open(path string) (*Store, error) {
	store := &Store{path: path, apps: make(map[string]App), now: time.Now}
	content, err := os.ReadFile(path) // #nosec G304 -- path is the fixed Dropserve state location.
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read dashboard preferences: %w", err)
	}
	var saved diskState
	if err := json.Unmarshal(content, &saved); err != nil {
		return nil, fmt.Errorf("parse dashboard preferences: %w", err)
	}
	if saved.Apps != nil {
		store.apps = saved.Apps
	}
	return store, nil
}

// Get returns one app's current settings.
func (store *Store) Get(slug string) App {
	store.mu.Lock()
	defer store.mu.Unlock()
	settings := store.apps[slug]
	if settings.Pinned != nil {
		value := *settings.Pinned
		settings.Pinned = &value
	}
	if settings.Hidden != nil {
		value := *settings.Hidden
		settings.Hidden = &value
	}
	return settings
}

// Set changes only the explicitly supplied values.
func (store *Store) Set(slug string, pinned, hidden *bool) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	settings := store.apps[slug]
	if pinned != nil {
		value := *pinned
		settings.Pinned = &value
	}
	if hidden != nil {
		value := *hidden
		settings.Hidden = &value
	}
	store.apps[slug] = settings
	return store.saveLocked()
}

// Touch records that the user opened an app.
func (store *Store) Touch(slug string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	settings := store.apps[slug]
	settings.LastUsed = store.now().UTC()
	store.apps[slug] = settings
	return store.saveLocked()
}

func (store *Store) saveLocked() error {
	content, err := json.MarshalIndent(diskState{Apps: store.apps}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dashboard preferences: %w", err)
	}
	content = append(content, '\n')
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create dashboard preferences directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "dashboard-*.tmp")
	if err != nil {
		return fmt.Errorf("create dashboard preferences file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect dashboard preferences: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write dashboard preferences: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync dashboard preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close dashboard preferences: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		backupPath := store.path + ".bak"
		_ = os.Remove(backupPath)
		if backupErr := os.Rename(store.path, backupPath); backupErr != nil && !errors.Is(backupErr, os.ErrNotExist) {
			return fmt.Errorf("protect existing dashboard preferences: %w", backupErr)
		}
		if replaceErr := os.Rename(temporaryPath, store.path); replaceErr != nil {
			_ = os.Rename(backupPath, store.path)
			return fmt.Errorf("replace dashboard preferences: %w", replaceErr)
		}
		_ = os.Remove(backupPath)
	}
	return nil
}
