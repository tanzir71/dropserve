package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch reloads config.toml after atomic replaces or in-place edits. Invalid
// edits are reported and ignored so the caller can keep its last good state.
func Watch(ctx context.Context, path string, onValid func(Config), onInvalid func(error)) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config watch directory: %w", err)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create config watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()
	if err := watcher.Add(directory); err != nil {
		return fmt.Errorf("watch config directory: %w", err)
	}

	var debounce *time.Timer
	var reload <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return nil
		case watchErr, open := <-watcher.Errors:
			if !open {
				return nil
			}
			if onInvalid != nil {
				onInvalid(fmt.Errorf("watch config: %w", watchErr))
			}
		case event, open := <-watcher.Events:
			if !open {
				return nil
			}
			if !samePath(event.Name, path) || event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if debounce == nil {
				debounce = time.NewTimer(200 * time.Millisecond)
			} else {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(200 * time.Millisecond)
			}
			reload = debounce.C
		case <-reload:
			reload = nil
			configuration, loadErr := Load(path)
			if loadErr != nil {
				if onInvalid != nil {
					onInvalid(loadErr)
				}
				continue
			}
			if onValid != nil {
				onValid(configuration)
			}
		}
	}
}
