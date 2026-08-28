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
	Kind        Kind
	Command     []string
	Runtime     string
	Reason      string
	Environment map[string]string
	BaseHref    string
	Autostart   bool
}

// Detect applies the ordered app-detection rules implemented so far.
func Detect(root string) (Detection, error) {
	settings, err := readManifestSettings(root)
	if err != nil {
		return Detection{}, err
	}
	procfileDetection, found, err := detectProcfile(root)
	if err != nil {
		return Detection{}, err
	}
	if found {
		return withManifestSettings(procfileDetection, settings), nil
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
			return withManifestSettings(Detection{
				Kind:      KindCommand,
				Command:   packageStartCommand(root),
				Runtime:   "node",
				Reason:    "Node app from package.json start script",
				Autostart: true,
			}, settings), nil
		}
		if entry, reason := packageNodeEntry(root, manifest.Main); entry != "" {
			return withManifestSettings(Detection{
				Kind:      KindCommand,
				Command:   []string{"node", entry},
				Runtime:   "node",
				Reason:    reason,
				Autostart: true,
			}, settings), nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Detection{}, fmt.Errorf("read %q: %w", packagePath, err)
	}
	php, err := detectPHP(root)
	if err != nil {
		return Detection{}, err
	}
	if php {
		return withManifestSettings(Detection{
			Kind:      KindPHP,
			Runtime:   "php",
			Reason:    "PHP app from PHP entry file",
			Autostart: true,
		}, settings), nil
	}
	for _, candidate := range []string{"app.py", "main.py", "server.py", "wsgi.py"} {
		info, statErr := os.Stat(filepath.Join(root, candidate))
		if statErr == nil && info.Mode().IsRegular() {
			return withManifestSettings(Detection{
				Kind:      KindCommand,
				Command:   []string{"python", candidate},
				Runtime:   "python",
				Reason:    "Python app from " + candidate,
				Autostart: true,
			}, settings), nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return Detection{}, fmt.Errorf("inspect %q: %w", filepath.Join(root, candidate), statErr)
		}
	}
	if executable, found, executableErr := singleExecutable(root); executableErr != nil {
		return Detection{}, executableErr
	} else if found {
		return withManifestSettings(Detection{
			Kind:      KindCommand,
			Command:   []string{executable},
			Reason:    "Command app from single executable " + filepath.Base(executable),
			Autostart: true,
		}, settings), nil
	}

	return withManifestSettings(Detection{Kind: KindStatic, Autostart: true}, settings), nil
}

func detectPHP(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, fmt.Errorf("inspect PHP app %q: %w", root, err)
	}
	hasPHP := false
	hasHTMLIndex := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if name == "index.php" {
			return true, nil
		}
		if name == "index.html" || name == "index.htm" {
			hasHTMLIndex = true
		}
		if filepath.Ext(name) == ".php" {
			hasPHP = true
		}
	}
	return hasPHP && !hasHTMLIndex, nil
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

type manifestSettings struct {
	Autostart   *bool             `json:"autostart"`
	Environment map[string]string `json:"env"`
	BaseHref    string            `json:"base_href"`
}

func readManifestSettings(root string) (manifestSettings, error) {
	manifestPath := filepath.Join(root, "dropserve.json")
	// #nosec G304 -- root is an app path from the read-only scanner.
	content, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return manifestSettings{}, nil
	}
	if err != nil {
		return manifestSettings{}, fmt.Errorf("read %q: %w", manifestPath, err)
	}
	var settings manifestSettings
	if err := json.Unmarshal(content, &settings); err != nil {
		return manifestSettings{}, fmt.Errorf("parse %q: %w", manifestPath, err)
	}
	return settings, nil
}

func withManifestSettings(detection Detection, settings manifestSettings) Detection {
	if settings.Autostart != nil {
		detection.Autostart = *settings.Autostart
	}
	if settings.Environment != nil {
		detection.Environment = make(map[string]string, len(settings.Environment))
		for name, value := range settings.Environment {
			detection.Environment[name] = value
		}
	}
	switch strings.ToLower(strings.TrimSpace(settings.BaseHref)) {
	case "always":
		detection.BaseHref = "always"
	case "never":
		detection.BaseHref = "never"
	default:
		detection.BaseHref = "auto"
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
