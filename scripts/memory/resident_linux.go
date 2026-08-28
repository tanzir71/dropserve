//go:build linux

package main

import (
	"fmt"
	"os"
)

func residentBytes() (uint64, error) {
	data, err := os.ReadFile("/proc/self/statm") // #nosec G304 -- fixed Linux procfs file for the current process.
	if err != nil {
		return 0, fmt.Errorf("read resident memory: %w", err)
	}
	var totalPages, residentPages uint64
	if _, err := fmt.Sscan(string(data), &totalPages, &residentPages); err != nil {
		return 0, fmt.Errorf("parse resident memory %q: %w", data, err)
	}
	return residentPages * uint64(os.Getpagesize()), nil
}
