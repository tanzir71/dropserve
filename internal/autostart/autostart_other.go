//go:build !windows && !linux && !darwin

// Package autostart manages Dropserve's per-user operating-system startup entry.
package autostart

import (
	"errors"
	"runtime"
)

func unsupportedError() error {
	return errors.New("autostart is not implemented on " + runtime.GOOS)
}

// Enable creates or replaces the current user's Dropserve startup entry.
func Enable(_ string) error {
	return unsupportedError()
}

// Disable removes the current user's Dropserve startup entry.
func Disable() error {
	return nil
}

// Enabled reports the actual presence of Dropserve's startup entry.
func Enabled() (bool, error) {
	return false, nil
}
