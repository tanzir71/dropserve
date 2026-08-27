package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const cacheVersion = 1

type cacheFile struct {
	Version int          `json:"version"`
	Entries []cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Entry
	FileNames []string `json:"file_names,omitempty"`
}

// Save atomically persists the complete in-memory search index.
func Save(path string, entries []Entry) error {
	cachedEntries := make([]cacheEntry, len(entries))
	for index, entry := range entries {
		cachedEntries[index] = cacheEntry{Entry: entry, FileNames: append([]string(nil), entry.fileNames...)}
	}
	content, err := json.MarshalIndent(cacheFile{Version: cacheVersion, Entries: cachedEntries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode index cache: %w", err)
	}
	content = append(content, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create index cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "index-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary index cache: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary index cache: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary index cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary index cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary index cache: %w", err)
	}
	if err := replaceCacheFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace index cache: %w", err)
	}
	return nil
}

// Load reads a previously persisted index, returning an empty slice when absent.
func Load(path string) ([]Entry, error) {
	// #nosec G304 -- path is Dropserve's configured state cache, not request data.
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read index cache: %w", err)
	}
	var cached cacheFile
	if err := json.Unmarshal(content, &cached); err != nil {
		return nil, fmt.Errorf("parse index cache: %w", err)
	}
	if cached.Version != cacheVersion {
		return nil, fmt.Errorf("index cache version %d is unsupported", cached.Version)
	}
	entries := make([]Entry, len(cached.Entries))
	for index, cachedEntry := range cached.Entries {
		entries[index] = cachedEntry.Entry
		entries[index].fileNames = append([]string(nil), cachedEntry.FileNames...)
	}
	return entries, nil
}

func replaceCacheFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) && runtime.GOOS != "windows" {
		return err
	}
	backup := destination + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(destination, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		_ = os.Rename(backup, destination)
		return err
	}
	_ = os.Remove(backup)
	return nil
}
