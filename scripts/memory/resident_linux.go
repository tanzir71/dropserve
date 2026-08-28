//go:build linux

package main

import (
	"fmt"
	"math"
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
	pageSize := os.Getpagesize()
	if pageSize <= 0 {
		return 0, fmt.Errorf("read resident memory: invalid page size %d", pageSize)
	}
	pageSizeBytes := uint64(pageSize) // #nosec G115 -- os.Getpagesize returned a checked positive value.
	if residentPages > math.MaxUint64/pageSizeBytes {
		return 0, fmt.Errorf("read resident memory: page count %d overflows bytes", residentPages)
	}
	return residentPages * pageSizeBytes, nil
}
