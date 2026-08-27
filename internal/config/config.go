// Package config loads and saves Dropserve's human-editable TOML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the complete user-editable configuration.
type Config struct {
	Server Server `toml:"server"`
}

// Server contains app discovery and listener settings.
type Server struct {
	AppsRoots      []string `toml:"apps_roots"`
	RegisteredApps []string `toml:"registered_apps,omitempty"`
	HTTPPort       int      `toml:"http_port"`
	HTTPSPort      int      `toml:"https_port"`
	Bind           string   `toml:"bind"`
	AppPortRange   []int    `toml:"app_port_range"`
}

// Default returns the zero-configuration product defaults.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Server: Server{
			AppsRoots:    []string{filepath.Join(home, "Dropserve", "Apps")},
			Bind:         "0.0.0.0",
			AppPortRange: []int{7400, 7999},
		},
	}
}

// DefaultPath returns the per-user configuration path.
func DefaultPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(root, "Dropserve", "config.toml"), nil
}

// Load reads path, returning defaults when it does not exist.
func Load(path string) (Config, error) {
	configuration := Default()
	// #nosec G304 -- path is Dropserve's configured per-user config path, not request data.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return configuration, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := toml.Unmarshal(data, &configuration); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return configuration, nil
}

// Register adds one external app folder and persists the updated config. It
// returns the absolute path and whether the config changed.
func Register(configPath, appPath string) (string, bool, error) {
	absolute, err := filepath.Abs(appPath)
	if err != nil {
		return "", false, fmt.Errorf("resolve app folder %q: %w", appPath, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", false, fmt.Errorf("open app folder %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("%q is a file; choose the folder that contains your app", absolute)
	}

	configuration, err := Load(configPath)
	if err != nil {
		return "", false, err
	}
	for _, registered := range configuration.Server.RegisteredApps {
		if samePath(registered, absolute) {
			return absolute, false, nil
		}
	}
	configuration.Server.RegisteredApps = append(configuration.Server.RegisteredApps, absolute)
	if err := Save(configPath, configuration); err != nil {
		return "", false, err
	}
	return absolute, true, nil
}

// Save writes a complete config through a sibling temporary file.
func Save(path string, configuration Config) error {
	data, err := toml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	return nil
}

func replaceFile(source, destination string) error {
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

func samePath(first, second string) bool {
	first = filepath.Clean(first)
	second = filepath.Clean(second)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(first, second)
	}
	return first == second
}
