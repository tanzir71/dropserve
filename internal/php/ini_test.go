package php

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteINIRegeneratesExistingSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "php", "php.ini")
	if err := WriteINI(path, "UTC"); err != nil {
		t.Fatalf("write initial PHP settings: %v", err)
	}
	if err := WriteINI(path, "Asia/Dhaka"); err != nil {
		t.Fatalf("regenerate PHP settings: %v", err)
	}
	content, err := os.ReadFile(path) // #nosec G304 -- path is inside this test's private temporary directory.
	if err != nil {
		t.Fatalf("read regenerated PHP settings: %v", err)
	}
	if !strings.Contains(string(content), `date.timezone = "Asia/Dhaka"`) {
		t.Fatalf("regenerated PHP settings = %q", content)
	}
}
