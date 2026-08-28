package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultFunnelLifetime = 8 * time.Hour

// ErrFunnelConfirmation means the typed confirmation did not exactly match
// the app slug.
var ErrFunnelConfirmation = errors.New("type the app slug exactly to confirm public sharing")

// FunnelAction describes one requested Tailscale CLI transition.
type FunnelAction struct {
	Slug   string
	Enable bool
}

// FunnelEntry is the persisted lifetime of one public app share.
type FunnelEntry struct {
	EnabledAt time.Time `json:"enabled_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// FunnelOptions supplies persistence, time, and the platform command boundary.
type FunnelOptions struct {
	StatePath string
	Clock     func() time.Time
	Lifetime  time.Duration
	Execute   func(context.Context, FunnelAction) error
}

// FunnelManager enforces public-sharing safety and owns expiring persisted
// state.
type FunnelManager struct {
	mu        sync.Mutex
	statePath string
	clock     func() time.Time
	lifetime  time.Duration
	execute   func(context.Context, FunnelAction) error
	entries   map[string]FunnelEntry
}

// NewFunnelManager creates a per-app Funnel controller and loads prior state.
func NewFunnelManager(options FunnelOptions) (*FunnelManager, error) {
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	lifetime := options.Lifetime
	if lifetime == 0 {
		lifetime = defaultFunnelLifetime
	}
	entries, err := loadFunnelState(options.StatePath)
	if err != nil {
		return nil, err
	}
	return &FunnelManager{
		statePath: options.StatePath,
		clock:     clock,
		lifetime:  lifetime,
		execute:   options.Execute,
		entries:   entries,
	}, nil
}

// Enable refuses every request whose session confirmation is not the exact
// app slug. The executor is unreachable before this check succeeds.
func (manager *FunnelManager) Enable(ctx context.Context, slug, confirmation string) error {
	if slug == "" || confirmation != slug {
		return ErrFunnelConfirmation
	}
	if manager == nil || manager.execute == nil {
		return errors.New("Tailscale Funnel is not configured") //nolint:staticcheck // Product name begins the user-facing error.
	}
	manager.mu.Lock()
	if entry, found := manager.entries[slug]; found && manager.clock().Before(entry.ExpiresAt) {
		manager.mu.Unlock()
		return nil
	}
	manager.mu.Unlock()

	if err := manager.execute(ctx, FunnelAction{Slug: slug, Enable: true}); err != nil {
		return err
	}
	now := manager.clock().UTC()
	manager.mu.Lock()
	manager.entries[slug] = FunnelEntry{EnabledAt: now, ExpiresAt: now.Add(manager.lifetime)}
	err := saveFunnelState(manager.statePath, manager.entries)
	if err != nil {
		delete(manager.entries, slug)
	}
	manager.mu.Unlock()
	if err != nil {
		_ = manager.execute(ctx, FunnelAction{Slug: slug, Enable: false})
		return err
	}
	return nil
}

// Active reports one persisted public share.
func (manager *FunnelManager) Active(slug string) (FunnelEntry, bool) {
	if manager == nil {
		return FunnelEntry{}, false
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	entry, found := manager.entries[slug]
	return entry, found && manager.clock().Before(entry.ExpiresAt)
}

// ActiveEntries returns an immutable copy of every unexpired public share.
func (manager *FunnelManager) ActiveEntries() map[string]FunnelEntry {
	active := make(map[string]FunnelEntry)
	if manager == nil {
		return active
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.clock()
	for slug, entry := range manager.entries {
		if now.Before(entry.ExpiresAt) {
			active[slug] = entry
		}
	}
	return active
}

// Expire disables and removes every share whose lifetime elapsed.
func (manager *FunnelManager) Expire(ctx context.Context) error {
	if manager == nil || manager.execute == nil {
		return errors.New("Tailscale Funnel is not configured") //nolint:staticcheck // Product name begins the user-facing error.
	}
	manager.mu.Lock()
	now := manager.clock()
	var expired []string
	for slug, entry := range manager.entries {
		if !now.Before(entry.ExpiresAt) {
			expired = append(expired, slug)
		}
	}
	manager.mu.Unlock()
	for _, slug := range expired {
		if err := manager.execute(ctx, FunnelAction{Slug: slug, Enable: false}); err != nil {
			return fmt.Errorf("disable expired Funnel for %s: %w", slug, err)
		}
		manager.mu.Lock()
		delete(manager.entries, slug)
		if err := saveFunnelState(manager.statePath, manager.entries); err != nil {
			manager.mu.Unlock()
			return err
		}
		manager.mu.Unlock()
	}
	return nil
}

func loadFunnelState(path string) (map[string]FunnelEntry, error) {
	entries := make(map[string]FunnelEntry)
	if path == "" {
		return entries, nil
	}
	// #nosec G304 -- path is the configured Dropserve machine-state path.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Funnel state: %w", err)
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse Funnel state: %w", err)
	}
	return entries, nil
}

func saveFunnelState(path string, entries map[string]FunnelEntry) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Funnel state: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Funnel state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "funnel-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Funnel state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary Funnel state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary Funnel state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary Funnel state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Funnel state: %w", err)
	}
	backup := path + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("back up Funnel state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Rename(backup, path)
		return fmt.Errorf("replace Funnel state: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}
