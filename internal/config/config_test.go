package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateChecksDefaultOnAndCanBeDisabled(t *testing.T) {
	if !Default().Updates.Check {
		t.Fatal("update checks do not default on")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[updates]\ncheck = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(path)
	if err != nil {
		t.Fatalf("load update setting: %v", err)
	}
	if configuration.Updates.Check {
		t.Fatal("updates.check=false was ignored")
	}
}

func TestWatchKeepsLastGoodConfigAcrossMalformedEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	valid := make(chan Config, 1)
	invalid := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, path, func(configuration Config) { valid <- configuration }, func(err error) { invalid <- err })
	}()
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("[dashboard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-invalid:
		if err == nil {
			t.Fatal("malformed edit reported nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("config watcher did not report malformed edit")
	}
	updated := Default()
	updated.Dashboard.Title = "Reloaded"
	if err := Save(path, updated); err != nil {
		t.Fatal(err)
	}
	select {
	case configuration := <-valid:
		if configuration.Dashboard.Title != "Reloaded" {
			t.Fatalf("reloaded config = %#v", configuration)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("config watcher did not publish corrected edit")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("config watcher did not stop with its context")
	}
}

func TestReferenceConfigurationSchemaLoadsAndValidates(t *testing.T) {
	pin := sha256.Sum256([]byte("123456"))
	content := `[server]
apps_roots = ["C:\\Apps"]
http_port = 8080
https_port = 8443
bind = "127.0.0.1"
app_port_range = [7500, 7600]

[dashboard]
title = "My Apps"
theme = "dark"
pin_to_root = "notes"

[discovery]
mdns = false
mdns_name = "my-dropserve"
tailscale = false

[security]
pin_enabled = true
pin_hash = "` + hex.EncodeToString(pin[:]) + `"

[runtimes]
php_version = "8.3"
lazy_start = "always"

[updates]
check = false
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(path)
	if err != nil {
		t.Fatalf("load reference schema: %v", err)
	}
	if configuration.Dashboard.Title != "My Apps" || configuration.Dashboard.Theme != "dark" || configuration.Dashboard.PinToRoot != "notes" {
		t.Fatalf("dashboard config = %#v", configuration.Dashboard)
	}
	if configuration.Discovery.MDNS || configuration.Discovery.Tailscale || configuration.Discovery.MDNSName != "my-dropserve" {
		t.Fatalf("discovery config = %#v", configuration.Discovery)
	}
	if !configuration.Security.PINEnabled || configuration.Runtimes.LazyStart != "always" || configuration.Updates.Check {
		t.Fatalf("remaining config = %#v", configuration)
	}
}

func TestInvalidSemanticConfigurationKeepsAUsefulError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[dashboard]\ntheme = \"ultraviolet\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "dashboard.theme") {
		t.Fatalf("semantic validation error = %v", err)
	}
}

func TestV1ConfigRejectsAnUnavailablePHPSeries(t *testing.T) {
	configuration := Default()
	configuration.Runtimes.PHPVersion = "8.5"
	if err := Validate(configuration); err == nil || !strings.Contains(err.Error(), "runtimes.php_version") {
		t.Fatalf("unavailable PHP series error = %v", err)
	}
}
