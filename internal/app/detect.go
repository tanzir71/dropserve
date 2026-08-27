package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	procfileDetection, found, err := detectProcfile(root)
	if err != nil {
		return Detection{}, err
	}
	if found {
		return withAutostart(procfileDetection, autostart), nil
	}
	packagePath := filepath.Join(root, "package.json")
	// #nosec G304 -- root is an app path from the read-only scanner.
	content, err := os.ReadFile(packagePath)
	if err == nil {
		var manifest struct {
			Main    string `json:"main"`
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
		if entry, reason := packageNodeEntry(root, manifest.Main); entry != "" {
			return withAutostart(Detection{
				Kind:      KindCommand,
				Command:   []string{"node", entry},
				Runtime:   "node",
				Reason:    reason,
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
	if executable, found, executableErr := singleExecutable(root); executableErr != nil {
		return Detection{}, executableErr
	} else if found {
		return withAutostart(Detection{
			Kind:      KindCommand,
			Command:   []string{executable},
			Reason:    "Command app from single executable " + filepath.Base(executable),
			Autostart: true,
		}, autostart), nil
	}

	return withAutostart(Detection{Kind: KindStatic, Autostart: true}, autostart), nil
}

func detectProcfile(root string) (Detection, bool, error) {
	procfilePath := filepath.Join(root, "Procfile")
	// #nosec G304 -- root is an app path from the read-only scanner.
	file, err := os.Open(procfilePath)
	if errors.Is(err, os.ErrNotExist) {
		return Detection{}, false, nil
	}
	if err != nil {
		return Detection{}, false, fmt.Errorf("read %q: %w", procfilePath, err)
	}
	defer func() {
		_ = file.Close()
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		kind, commandLine, found := strings.Cut(scanner.Text(), ":")
		if !found || strings.TrimSpace(kind) != "web" {
			continue
		}
		command := strings.Fields(strings.TrimSpace(commandLine))
		if len(command) == 0 {
			continue
		}
		return Detection{
			Kind:      KindCommand,
			Command:   command,
			Runtime:   commandRuntime(command[0]),
			Reason:    "Command app from Procfile web entry",
			Autostart: true,
		}, true, nil
	}
	if err := scanner.Err(); err != nil {
		return Detection{}, false, fmt.Errorf("scan %q: %w", procfilePath, err)
	}
	return Detection{}, false, nil
}

func packageNodeEntry(root, mainEntry string) (string, string) {
	if mainEntry != "" {
		entry := filepath.Clean(filepath.FromSlash(mainEntry))
		if isRegularFile(filepath.Join(root, entry)) {
			return entry, "Node app from package.json main entry"
		}
	}
	for _, candidate := range []string{"index.js", "server.js"} {
		if isRegularFile(filepath.Join(root, candidate)) {
			return candidate, "Node app from " + candidate
		}
	}
	return "", ""
}

func singleExecutable(root string) (string, bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false, fmt.Errorf("inspect executable app %q: %w", root, err)
	}
	var candidate os.DirEntry
	for _, entry := range entries {
		if entry.Name() == "dropserve.json" {
			continue
		}
		if candidate != nil {
			return "", false, nil
		}
		candidate = entry
	}
	if candidate == nil {
		return "", false, nil
	}
	info, err := candidate.Info()
	if err != nil {
		return "", false, fmt.Errorf("inspect executable %q: %w", filepath.Join(root, candidate.Name()), err)
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	isExecutable := runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(candidate.Name()), ".exe")
	if runtime.GOOS != "windows" {
		isExecutable = info.Mode().Perm()&0o111 != 0
	}
	if !isExecutable {
		return "", false, nil
	}
	return filepath.Join(root, candidate.Name()), true, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func commandRuntime(command string) string {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe")
	switch name {
	case "node", "npm", "npx", "pnpm", "yarn", "bun":
		return "node"
	case "python", "python3", "py":
		return "python"
	default:
		return name
	}
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
