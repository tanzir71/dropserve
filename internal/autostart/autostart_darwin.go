//go:build darwin

// Package autostart manages Dropserve's per-user launch agent.
package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const launchAgentFileName = launchAgentLabel + ".plist"

// Enable writes, loads, and starts the current user's Dropserve LaunchAgent.
func Enable(executable string) error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	domain := launchAgentDomain()
	target := domain + "/" + launchAgentLabel
	if output, err := runLaunchctl("bootout", target); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return launchctlError("replace the existing launch agent", output, err)
		}
	}
	if err := writeLaunchAgent(path, makeLaunchAgent(executable)); err != nil {
		return err
	}
	if output, err := runLaunchctl("bootstrap", domain, path); err != nil {
		return launchctlError("load the launch agent", output, err)
	}
	if output, err := runLaunchctl("kickstart", "-k", target); err != nil {
		return launchctlError("start the launch agent", output, err)
	}
	return nil
}

// Disable unloads and removes the current user's Dropserve LaunchAgent.
func Disable() error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	target := launchAgentDomain() + "/" + launchAgentLabel
	if output, err := runLaunchctl("bootout", target); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return launchctlError("unload the launch agent", output, err)
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove launch agent: %w", err)
	}
	return nil
}

// Enabled asks launchd whether the current user's Dropserve agent is loaded.
func Enabled() (bool, error) {
	target := launchAgentDomain() + "/" + launchAgentLabel
	output, err := runLaunchctl("print", target)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, launchctlError("query the launch agent", output, err)
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home folder: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentFileName), nil
}

func launchAgentDomain() string {
	return fmt.Sprintf("gui/%d", syscall.Getuid())
}

func writeLaunchAgent(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".dropserve-agent-*.plist")
	if err != nil {
		return fmt.Errorf("create temporary launch agent: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set launch agent permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write launch agent: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close launch agent: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install launch agent: %w", err)
	}
	return nil
}

func runLaunchctl(arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// #nosec G204 -- executable and arguments are fixed package-controlled launchd operations.
	command := exec.CommandContext(ctx, "launchctl", arguments...)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, fmt.Errorf("launchctl timed out: %w", ctx.Err())
	}
	return output, err
}

func launchctlError(action string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}
