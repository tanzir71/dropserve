package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Detection is the first matching rule for one app directory.
type Detection struct {
	Kind      Kind
	Command   []string
	Runtime   string
	Reason    string
	Autostart bool
}

// Detect applies the ordered app-detection rules implemented so far.
func Detect(root string) (Detection, error) {
	packagePath := filepath.Join(root, "package.json")
	// #nosec G304 -- root is an app path from the read-only scanner.
	content, err := os.ReadFile(packagePath)
	if err == nil {
		var manifest struct {
			Scripts struct {
				Start string `json:"start"`
			} `json:"scripts"`
		}
		if err := json.Unmarshal(content, &manifest); err != nil {
			return Detection{}, fmt.Errorf("parse %q: %w", packagePath, err)
		}
		if manifest.Scripts.Start != "" {
			return Detection{
				Kind:      KindCommand,
				Command:   packageStartCommand(root),
				Runtime:   "node",
				Reason:    "Node app from package.json start script",
				Autostart: true,
			}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Detection{}, fmt.Errorf("read %q: %w", packagePath, err)
	}

	return Detection{Kind: KindStatic, Autostart: true}, nil
}

func packageStartCommand(root string) []string {
	for _, candidate := range []struct {
		name    string
		command []string
	}{
		{name: "pnpm-lock.yaml", command: []string{"pnpm", "start"}},
		{name: "yarn.lock", command: []string{"yarn", "start"}},
		{name: "bun.lock", command: []string{"bun", "run", "start"}},
		{name: "bun.lockb", command: []string{"bun", "run", "start"}},
	} {
		if _, err := os.Stat(filepath.Join(root, candidate.name)); err == nil {
			return candidate.command
		}
	}
	return []string{"npm", "start"}
}
