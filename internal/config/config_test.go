package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateChecksDefaultOnAndCanBeDisabled(t *testing.T) {
	if !Default().Updates.Check {
		t.Fatal("update checks do not default on")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[updates]\ncheck = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := Load(path)
	if err != nil {
		t.Fatalf("load update setting: %v", err)
	}
	if configuration.Updates.Check {
		t.Fatal("updates.check=false was ignored")
	}
}
