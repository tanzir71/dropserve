//go:build !linux && !windows

package main

import (
	"fmt"
	"runtime"
)

func residentBytes() (uint64, error) {
	return 0, fmt.Errorf("resident-memory measurement is unsupported on %s", runtime.GOOS)
}
