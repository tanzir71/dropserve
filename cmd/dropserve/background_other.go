//go:build !windows

package main

func backgroundConsoleWindow() uintptr {
	return 0
}

func backgroundExecutable(current string) string {
	return current
}
