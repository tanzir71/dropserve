package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Detection is the first matching rule for one app directory.
type Detection struct {
	Kind             Kind
	Command          []string
	Runtime          string
	Reason           string
	Name             string
	Description      string
	Icon             string
	Tags             []string
	Environment      map[string]string
	HealthPath       string
	PortEnv          string
	Index            *string
	SPA              bool
	DirectoryListing *bool
	BaseHref         string
	Autostart        bool
	Visibility       string
	Pinned           bool
	Hidden           bool
	Warnings         []ManifestWarning
}

// ManifestWarning is a non-fatal problem in dropserve.json. Detection remains
// available so one bad optional setting never makes an app disappear.
type ManifestWarning struct {
	Code    string
	Message string
}

// Detect applies the ordered app-detection rules implemented so far.
func Detect(root string) (Detection, error) {
	settings, err := readManifestSettings(root)
	if err != nil {
		return Detection{}, err
	}
	switch strings.ToLower(strings.TrimSpace(settings.Type)) {
	case "static":
		return withManifestSettings(Detection{Kind: KindStatic, Autostart: true}, settings), nil
	case "php":
		return withManifestSettings(Detection{Kind: KindPHP, Runtime: "php", Autostart: true}, settings), nil
	case "command":
		if command, commandErr := splitCommandLine(settings.Command); commandErr == nil && len(command) != 0 {
			return withManifestSettings(Detection{Kind: KindCommand, Command: command, Runtime: commandRuntime(command[0]), Autostart: true}, settings), nil
		}
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
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Icon             string            `json:"icon"`
	Tags             []string          `json:"tags"`
	Type             string            `json:"type"`
	Command          string            `json:"command"`
	PortEnv          string            `json:"port_env"`
	Environment      map[string]string `json:"env"`
	HealthPath       string            `json:"health_path"`
	Autostart        *bool             `json:"autostart"`
	Index            *string           `json:"index"`
	SPA              bool              `json:"spa"`
	DirectoryListing *bool             `json:"directory_listing"`
	BaseHref         string            `json:"base_href"`
	Visibility       string            `json:"visibility"`
	Pinned           bool              `json:"pinned"`
	Hidden           bool              `json:"hidden"`
	Warnings         []ManifestWarning `json:"-"`
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
		return manifestSettings{Warnings: []ManifestWarning{{
			Code:    "manifest_parse",
			Message: fmt.Sprintf("Could not parse %s; Dropserve used automatic detection instead: %v", manifestPath, err),
		}}}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err == nil {
		known := map[string]struct{}{
			"name": {}, "description": {}, "icon": {}, "tags": {}, "type": {}, "command": {},
			"port_env": {}, "env": {}, "health_path": {}, "autostart": {}, "index": {}, "spa": {},
			"directory_listing": {}, "base_href": {}, "visibility": {}, "pinned": {}, "hidden": {},
		}
		for key := range fields {
			if _, ok := known[key]; !ok {
				settings.Warnings = append(settings.Warnings, ManifestWarning{
					Code:    "manifest_unknown_key",
					Message: fmt.Sprintf("dropserve.json ignores unknown key %q", key),
				})
			}
		}
	}
	return settings, nil
}

func withManifestSettings(detection Detection, settings manifestSettings) Detection {
	detection.Warnings = append(detection.Warnings, settings.Warnings...)
	detection.Name = strings.TrimSpace(settings.Name)
	detection.Description = strings.TrimSpace(settings.Description)
	if icon := strings.TrimSpace(settings.Icon); icon != "" {
		if validManifestRelativePath(icon) {
			detection.Icon = filepath.ToSlash(icon)
		} else {
			detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_icon", Message: fmt.Sprintf("dropserve.json icon %q must stay inside the app; using a generated icon", settings.Icon)})
		}
	}
	detection.Tags = cleanManifestTags(settings.Tags)
	if settings.Autostart != nil {
		detection.Autostart = *settings.Autostart
	}
	if settings.Environment != nil {
		detection.Environment = make(map[string]string, len(settings.Environment))
		for name, value := range settings.Environment {
			detection.Environment[name] = value
		}
	}
	if healthPath := strings.TrimSpace(settings.HealthPath); healthPath != "" {
		if strings.HasPrefix(healthPath, "/") {
			detection.HealthPath = healthPath
		} else {
			detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_health_path", Message: "dropserve.json health_path must start with /; using /"})
		}
	}
	if portEnv := strings.TrimSpace(settings.PortEnv); portEnv != "" {
		if validEnvironmentName(portEnv) {
			detection.PortEnv = portEnv
		} else {
			detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_port_env", Message: fmt.Sprintf("dropserve.json port_env %q is not a valid environment variable name; using PORT", portEnv)})
		}
	}
	if settings.Index != nil {
		index := filepath.ToSlash(strings.TrimSpace(*settings.Index))
		if validManifestRelativePath(index) {
			detection.Index = &index
		} else {
			detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_index", Message: fmt.Sprintf("dropserve.json index %q must stay inside the app; using automatic index detection", *settings.Index)})
		}
	}
	detection.SPA = settings.SPA
	detection.DirectoryListing = settings.DirectoryListing
	switch strings.ToLower(strings.TrimSpace(settings.BaseHref)) {
	case "always":
		detection.BaseHref = "always"
	case "never":
		detection.BaseHref = "never"
	case "", "auto":
		detection.BaseHref = "auto"
	default:
		detection.BaseHref = "auto"
		detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_base_href", Message: fmt.Sprintf("dropserve.json base_href %q is invalid; using auto", settings.BaseHref)})
	}
	switch visibility := strings.ToLower(strings.TrimSpace(settings.Visibility)); visibility {
	case "", "lan":
		detection.Visibility = "lan"
	case "local", "tailnet", "public":
		detection.Visibility = visibility
	default:
		detection.Visibility = "lan"
		detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_visibility", Message: fmt.Sprintf("dropserve.json visibility %q is invalid; using lan", settings.Visibility)})
	}
	detection.Pinned = settings.Pinned
	detection.Hidden = settings.Hidden

	manifestType := strings.ToLower(strings.TrimSpace(settings.Type))
	manifestCommand, commandErr := splitCommandLine(settings.Command)
	if commandErr != nil {
		detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_command", Message: "dropserve.json command has unmatched quoting; using automatic detection"})
		manifestCommand = nil
	}
	if manifestType != "" {
		switch Kind(manifestType) {
		case KindStatic:
			detection.Kind = KindStatic
			detection.Command = nil
			detection.Runtime = ""
			detection.Reason = "Static app from dropserve.json type override"
		case KindPHP:
			detection.Kind = KindPHP
			detection.Command = nil
			detection.Runtime = "php"
			detection.Reason = "PHP app from dropserve.json type override"
		case KindCommand:
			command := manifestCommand
			if len(command) == 0 && detection.Kind != KindCommand {
				detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_command", Message: "dropserve.json type command needs a non-empty command; using automatic detection"})
			} else {
				detection.Kind = KindCommand
				if len(command) != 0 {
					detection.Command = command
					detection.Runtime = commandRuntime(command[0])
				}
				detection.Reason = "Command app from dropserve.json type override"
			}
		default:
			detection.Warnings = append(detection.Warnings, ManifestWarning{Code: "manifest_type", Message: fmt.Sprintf("dropserve.json type %q is invalid; using automatic detection", settings.Type)})
		}
	} else if command := manifestCommand; len(command) != 0 && detection.Kind == KindCommand {
		detection.Command = command
		detection.Runtime = commandRuntime(command[0])
		detection.Reason = "Command app with dropserve.json command override"
	}
	return detection
}

func splitCommandLine(value string) ([]string, error) {
	var arguments []string
	var current strings.Builder
	var quote rune
	started := false
	flush := func() {
		if started {
			arguments = append(arguments, current.String())
			current.Reset()
			started = false
		}
	}
	characters := []rune(strings.TrimSpace(value))
	for index := 0; index < len(characters); index++ {
		character := characters[index]
		if character == '\\' && quote != '\'' {
			if index+1 < len(characters) {
				next := characters[index+1]
				if next == '\\' || next == quote || (quote == 0 && strings.ContainsRune(" \t\r\n", next)) {
					current.WriteRune(next)
					index++
					started = true
					continue
				}
			}
			current.WriteRune(character)
			started = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				current.WriteRune(character)
			}
			started = true
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
			started = true
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteRune(character)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unmatched command quoting")
	}
	flush()
	return arguments, nil
}

func cleanManifestTags(tags []string) []string {
	cleaned := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, tag)
	}
	return cleaned
}

func validEnvironmentName(name string) bool {
	for index, character := range name {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return name != ""
}

func validManifestRelativePath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
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
