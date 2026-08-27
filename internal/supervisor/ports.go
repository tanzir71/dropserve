package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	firstAppPort = 7400
	lastAppPort  = 7999
)

type portRegistry struct {
	path     string
	assigned map[string]int
}

var processPortReservations = struct {
	sync.Mutex
	owners map[int]*portRegistry
}{owners: make(map[int]*portRegistry)}

func newPortRegistry(path string) (*portRegistry, error) {
	registry := &portRegistry{path: path, assigned: make(map[string]int)}
	if path == "" {
		return registry, nil
	}
	// #nosec G304 -- path is derived from Dropserve's configured state directory.
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read app ports: %w", err)
	}
	if err := json.Unmarshal(content, &registry.assigned); err != nil {
		return nil, fmt.Errorf("parse app ports: %w", err)
	}
	for slug, port := range registry.assigned {
		if port < firstAppPort || port > lastAppPort {
			delete(registry.assigned, slug)
		}
	}
	return registry, nil
}

func (registry *portRegistry) assign(slug string) (int, error) {
	processPortReservations.Lock()
	defer processPortReservations.Unlock()
	if persisted := registry.assigned[slug]; persisted != 0 && registry.canClaim(persisted) {
		processPortReservations.owners[persisted] = registry
		return persisted, nil
	}
	used := make(map[int]struct{}, len(registry.assigned))
	for assignedSlug, port := range registry.assigned {
		if assignedSlug != slug {
			used[port] = struct{}{}
		}
	}
	for port := firstAppPort; port <= lastAppPort; port++ {
		if _, found := used[port]; found || !registry.canClaim(port) {
			continue
		}
		registry.assigned[slug] = port
		processPortReservations.owners[port] = registry
		if err := registry.save(); err != nil {
			delete(processPortReservations.owners, port)
			return 0, err
		}
		return port, nil
	}
	return 0, errors.New("no app port is available from 7400 through 7999")
}

func (registry *portRegistry) canClaim(port int) bool {
	owner := processPortReservations.owners[port]
	return (owner == nil || owner == registry) && portAvailable(port)
}

func (registry *portRegistry) release() {
	processPortReservations.Lock()
	defer processPortReservations.Unlock()
	for port, owner := range processPortReservations.owners {
		if owner == registry {
			delete(processPortReservations.owners, port)
		}
	}
}

func (registry *portRegistry) save() error {
	if registry.path == "" {
		return nil
	}
	content, err := json.MarshalIndent(registry.assigned, "", "  ")
	if err != nil {
		return fmt.Errorf("encode app ports: %w", err)
	}
	content = append(content, '\n')
	directory := filepath.Dir(registry.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create app-port state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "ports-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary app ports: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary app ports: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary app ports: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary app ports: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary app ports: %w", err)
	}
	backup := registry.path + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(registry.path, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("back up app ports: %w", err)
	}
	if err := os.Rename(temporaryPath, registry.path); err != nil {
		_ = os.Rename(backup, registry.path)
		return fmt.Errorf("replace app ports: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func portAvailable(port int) bool {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(
		context.Background(),
		"tcp",
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
	)
	if err != nil {
		return false
	}
	return listener.Close() == nil
}
