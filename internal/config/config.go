// Package config loads and saves Dropserve's human-editable TOML configuration.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the complete user-editable configuration.
type Config struct {
	Server    Server    `toml:"server"`
	Dashboard Dashboard `toml:"dashboard"`
	Discovery Discovery `toml:"discovery"`
	Security  Security  `toml:"security"`
	Runtimes  Runtimes  `toml:"runtimes"`
	Updates   Updates   `toml:"updates"`
}

// Updates controls the metadata-only GitHub release check.
type Updates struct {
	Check bool `toml:"check"`
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

// Dashboard controls the embedded launcher presentation.
type Dashboard struct {
	Title     string `toml:"title"`
	Theme     string `toml:"theme"`
	PinToRoot string `toml:"pin_to_root"`
}

// Discovery controls optional local-name and installed-Tailscale integration.
type Discovery struct {
	MDNS      bool   `toml:"mdns"`
	MDNSName  string `toml:"mdns_name"`
	Tailscale bool   `toml:"tailscale"`
}

// Security controls the optional non-loopback PIN gate.
type Security struct {
	PINEnabled bool   `toml:"pin_enabled"`
	PINHash    string `toml:"pin_hash"`
}

// Runtimes controls optional runtime selection and command-app startup.
type Runtimes struct {
	PHPVersion string `toml:"php_version"`
	LazyStart  string `toml:"lazy_start"`
}

// Default returns the zero-configuration product defaults.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Dashboard: Dashboard{Title: "Dropserve", Theme: "auto"},
		Discovery: Discovery{MDNS: true, MDNSName: "dropserve", Tailscale: true},
		Runtimes:  Runtimes{PHPVersion: "8.3", LazyStart: "auto"},
		Updates:   Updates{Check: true},
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
	if err := Validate(configuration); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}
	return configuration, nil
}

// Validate checks values that TOML syntax alone cannot constrain.
func Validate(configuration Config) error {
	if configuration.Server.Bind == "" || net.ParseIP(configuration.Server.Bind) == nil {
		return fmt.Errorf("server.bind must be an IP address such as 0.0.0.0 or 127.0.0.1")
	}
	for name, port := range map[string]int{"server.http_port": configuration.Server.HTTPPort, "server.https_port": configuration.Server.HTTPSPort} {
		if port < 0 || port > 65_535 {
			return fmt.Errorf("%s must be between 0 and 65535", name)
		}
	}
	if len(configuration.Server.AppPortRange) != 2 || configuration.Server.AppPortRange[0] < 1 || configuration.Server.AppPortRange[1] > 65_535 || configuration.Server.AppPortRange[0] > configuration.Server.AppPortRange[1] {
		return fmt.Errorf("server.app_port_range must contain two ascending ports between 1 and 65535")
	}
	if configuration.Dashboard.Title = strings.TrimSpace(configuration.Dashboard.Title); configuration.Dashboard.Title == "" {
		return fmt.Errorf("dashboard.title must not be empty")
	}
	switch strings.ToLower(strings.TrimSpace(configuration.Dashboard.Theme)) {
	case "auto", "light", "dark":
	default:
		return fmt.Errorf("dashboard.theme must be auto, light, or dark")
	}
	if slug := strings.TrimSpace(configuration.Dashboard.PinToRoot); slug != "" && !validSlug(slug) {
		return fmt.Errorf("dashboard.pin_to_root must be a lowercase app slug")
	}
	if !validMDNSName(configuration.Discovery.MDNSName) {
		return fmt.Errorf("discovery.mdns_name must be one DNS label such as dropserve")
	}
	if configuration.Security.PINEnabled {
		decoded, err := hex.DecodeString(configuration.Security.PINHash)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("security.pin_hash must be a 64-character SHA-256 hex digest when PIN lock is enabled")
		}
	}
	switch strings.ToLower(strings.TrimSpace(configuration.Runtimes.LazyStart)) {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("runtimes.lazy_start must be auto, always, or never")
	}
	if strings.TrimSpace(configuration.Runtimes.PHPVersion) != "8.3" {
		return fmt.Errorf("runtimes.php_version must be 8.3 in Dropserve v1")
	}
	return nil
}

func validSlug(value string) bool {
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || (character == '-' && index > 0 && index < len(value)-1) {
			continue
		}
		return false
	}
	return value != "" && !strings.Contains(value, "--")
}

func validMDNSName(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".local")
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || (character == '-' && index > 0 && index < len(value)-1) {
			continue
		}
		return false
	}
	return true
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
	if err := Validate(configuration); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
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
