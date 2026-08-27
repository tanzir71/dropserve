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
	autostart, err := manifestAutostart(root)
	if err != nil {
		return Detection{}, err
	}
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
			return withAutostart(Detection{
				Kind:      KindCommand,
				Command:   packageStartCommand(root),
				Runtime:   "node",
				Reason:    "Node app from package.json start script",
				Autostart: true,
			}, autostart), nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Detection{}, fmt.Errorf("read %q: %w", packagePath, err)
	}
	for _, candidate := range []string{"app.py", "main.py", "server.py", "wsgi.py"} {
		info, statErr := os.Stat(filepath.Join(root, candidate))
		if statErr == nil && info.Mode().IsRegular() {
			return withAutostart(Detection{
				Kind:      KindCommand,
				Command:   []string{"python", candidate},
				Runtime:   "python",
				Reason:    "Python app from " + candidate,
				Autostart: true,
			}, autostart), nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return Detection{}, fmt.Errorf("inspect %q: %w", filepath.Join(root, candidate), statErr)
		}
	}

	return withAutostart(Detection{Kind: KindStatic, Autostart: true}, autostart), nil
}

func manifestAutostart(root string) (*bool, error) {
	manifestPath := filepath.Join(root, "dropserve.json")
	// #nosec G304 -- root is an app path from the read-only scanner.
	content, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", manifestPath, err)
	}
	var manifest struct {
		Autostart *bool `json:"autostart"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parse %q: %w", manifestPath, err)
	}
	return manifest.Autostart, nil
}

func withAutostart(detection Detection, autostart *bool) Detection {
	if autostart != nil {
		detection.Autostart = *autostart
	}
	return detection
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
