package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConsoleVariantRegistersGUIVariantForAutostart(t *testing.T) {
	directory := t.TempDir()
	console := filepath.Join(directory, "dropserve-cli.exe")
	gui := filepath.Join(directory, "dropserve.exe")
	if err := os.WriteFile(gui, []byte("test binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := backgroundExecutable(console); got != gui {
		t.Fatalf("backgroundExecutable(%q) = %q, want %q", console, got, gui)
	}
}

func TestConsoleVariantFallsBackWhenGUIVariantIsMissing(t *testing.T) {
	console := filepath.Join(t.TempDir(), "dropserve-cli.exe")
	if got := backgroundExecutable(console); got != console {
		t.Fatalf("backgroundExecutable(%q) = %q, want the current executable", console, got)
	}
}
