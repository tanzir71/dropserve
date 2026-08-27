//go:build !windows

package server_test

import (
	"errors"
	"syscall"
)

func processAlive(processID uint32) (bool, error) {
	err := syscall.Kill(int(processID), syscall.Signal(0)) // #nosec G115 -- process IDs fit the platform's signed integer range.
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}
