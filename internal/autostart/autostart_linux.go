//go:build linux

// Package autostart manages Dropserve's per-user systemd service.
package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const systemdUnitName = "dropserve.service"

// Enable writes, enables, and starts the current user's Dropserve service.
func Enable(executable string) error {
	path, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := writeSystemdUnit(path, makeSystemdUnit(executable)); err != nil {
		return err
	}
	if output, err := runSystemctl("--user", "daemon-reload"); err != nil {
		return commandError("reload the systemd user manager", output, err)
	}
	if output, err := runSystemctl("--user", "enable", "--now", systemdUnitName); err != nil {
		return commandError("enable the systemd user service", output, err)
	}
	return nil
}

// Disable stops, disables, and removes the current user's Dropserve service.
func Disable() error {
	path, err := systemdUnitPath()
	if err != nil {
		return err
	}
	output, disableErr := runSystemctl("--user", "disable", "--now", systemdUnitName)
	var exitErr *exec.ExitError
	if disableErr != nil && !errors.As(disableErr, &exitErr) {
		return commandError("disable the systemd user service", output, disableErr)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove systemd user unit: %w", err)
	}
	if output, err := runSystemctl("--user", "daemon-reload"); err != nil {
		return commandError("reload the systemd user manager", output, err)
	}
	return nil
}

// Enabled asks systemd whether Dropserve's current-user service is enabled.
func Enabled() (bool, error) {
	output, err := runSystemctl("--user", "is-enabled", "--quiet", systemdUnitName)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, commandError("query the systemd user service", output, err)
}

func systemdUnitPath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the user configuration folder: %w", err)
	}
	return filepath.Join(configDirectory, "systemd", "user", systemdUnitName), nil
}

func writeSystemdUnit(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create systemd user directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".dropserve.service-*")
	if err != nil {
		return fmt.Errorf("create temporary systemd user unit: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set systemd user unit permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write systemd user unit: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close systemd user unit: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install systemd user unit: %w", err)
	}
	return nil
}

func runSystemctl(arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "systemctl", arguments...) // #nosec G204 -- executable and arguments are fixed package-controlled systemd operations.
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, fmt.Errorf("systemctl timed out: %w", ctx.Err())
	}
	return output, err
}

func commandError(action string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}
