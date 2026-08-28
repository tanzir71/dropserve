// Package doctor builds Dropserve's complete, read-only support report.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/tanzir71/dropserve/internal/app"
	"github.com/tanzir71/dropserve/internal/autostart"
	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/scanner"
	"github.com/tanzir71/dropserve/internal/state"
)

const diagnosticTimeout = 10 * time.Second

type severity int

const (
	severityOK severity = iota
	severityWarning
	severityFailure
)

// Options names the local files and build identity inspected by Diagnose.
type Options struct {
	ConfigPath   string
	StatePath    string
	LogDirectory string
	Version      string
	Commit       string
}

// Probes supplies platform boundaries. Zero fields use production probes.
type Probes struct {
	OS               string
	LookPath         func(string) (string, error)
	RunCommand       func(string, ...string) ([]byte, error)
	ProbeMDNS        func() error
	AutostartEnabled func() (bool, error)
}

type check struct {
	severity severity
	label    string
	detail   string
	required bool
}

// Report is the ordered diagnostic output and its required-failure state.
type Report struct {
	checks []check
}

// Diagnose inspects the local Dropserve setup without changing it.
func Diagnose(options Options, probes Probes) Report {
	probes = completeProbes(probes)
	report := Report{}
	report.add(severityOK, "Version", fmt.Sprintf("%s (%s)", options.Version, options.Commit), false)

	configuration, configErr := config.Load(options.ConfigPath)
	if configErr != nil {
		report.add(severityFailure, "Configuration", configErr.Error(), true)
		configuration = config.Default()
	} else {
		report.add(severityOK, "Configuration", options.ConfigPath, false)
	}

	if options.StatePath == "" {
		var err error
		options.StatePath, err = state.DefaultPath()
		if err != nil {
			report.add(severityFailure, "HTTP port", "runtime state path is unavailable: "+err.Error(), true)
		}
	}
	snapshot, stateErr := state.Load(options.StatePath)
	if stateErr != nil {
		report.add(severityFailure, "HTTP port", stateErr.Error(), true)
	} else {
		report.addCheck(portCheck(configuration, snapshot))
	}

	addWindowsChecks(&report, probes)
	addRootChecks(&report, configuration)

	scan, scanErr := scanner.Scan(scanner.Options{
		Roots:      configuration.Server.AppsRoots,
		Registered: configuration.Server.RegisteredApps,
	})
	if scanErr != nil {
		report.add(severityFailure, "Apps", scanErr.Error(), true)
	} else {
		addAppChecks(&report, scan)
	}
	addRuntimeChecks(&report, scan.Apps, probes)
	addMDNSCheck(&report, probes)
	addTailscaleCheck(&report, probes)
	addAutostartCheck(&report, probes)

	if options.LogDirectory == "" && options.StatePath != "" {
		options.LogDirectory = filepath.Join(filepath.Dir(options.StatePath), "logs")
	}
	addLogChecks(&report, options.LogDirectory)
	return report
}

// RequiredFailure reports whether a condition needed to serve configured apps failed.
func (report Report) RequiredFailure() bool {
	for _, item := range report.checks {
		if item.required && item.severity == severityFailure {
			return true
		}
	}
	return false
}

// Write renders one plain-language diagnostic per line.
func (report Report) Write(output io.Writer) error {
	for _, item := range report.checks {
		status := "[OK]"
		switch item.severity {
		case severityWarning:
			status = "[WARN]"
		case severityFailure:
			status = "[FAIL]"
		}
		if _, err := fmt.Fprintf(output, "%s %s: %s\n", status, item.label, item.detail); err != nil {
			return err
		}
	}
	return nil
}

func (report *Report) add(itemSeverity severity, label, detail string, required bool) {
	report.checks = append(report.checks, check{
		severity: itemSeverity,
		label:    label,
		detail:   detail,
		required: required,
	})
}

func (report *Report) addCheck(item check) {
	report.checks = append(report.checks, item)
}

func completeProbes(probes Probes) Probes {
	if probes.OS == "" {
		probes.OS = runtime.GOOS
	}
	if probes.LookPath == nil {
		probes.LookPath = exec.LookPath
	}
	if probes.RunCommand == nil {
		probes.RunCommand = runCommand
	}
	if probes.ProbeMDNS == nil {
		probes.ProbeMDNS = probeMDNS
	}
	if probes.AutostartEnabled == nil {
		probes.AutostartEnabled = autostart.Enabled
	}
	return probes
}

func portCheck(configuration config.Config, snapshot state.State) check {
	port := snapshot.HTTPPort
	if port == 0 {
		port = configuration.Server.HTTPPort
	}
	if port == 0 {
		return check{severity: severityWarning, label: "HTTP port", detail: "not selected yet; automatic selection will try port 80 first"}
	}
	reason := "preferred port 80"
	if port != 80 {
		reason = "configured explicitly; port 80 was not requested"
		if configuration.Server.HTTPPort == 0 {
			reason = "selected automatically because port 80 was unavailable"
			for _, warning := range snapshot.Warnings {
				if warning.Code == "port_fallback" && warning.Message != "" {
					reason = warning.Message
					break
				}
			}
		}
	}
	return check{severity: severityOK, label: "HTTP port", detail: fmt.Sprintf("%d — %s", port, reason)}
}

func addWindowsChecks(report *Report, probes Probes) {
	if probes.OS != "windows" {
		report.add(severityOK, "Windows excluded TCP port ranges", "not applicable on "+probes.OS, false)
		report.add(severityOK, "Windows firewall rule", "not applicable on "+probes.OS, false)
		return
	}
	output, err := probes.RunCommand("netsh.exe", "interface", "ipv4", "show", "excludedportrange", "protocol=tcp")
	if err != nil {
		report.add(severityWarning, "Windows excluded TCP port ranges", err.Error(), false)
	} else {
		report.add(severityOK, "Windows excluded TCP port ranges", summarize(output), false)
	}
	output, err = probes.RunCommand("netsh.exe", "advfirewall", "firewall", "show", "rule", "name=Dropserve")
	if err != nil {
		report.add(severityWarning, "Windows firewall rule", "not found or unreadable: "+err.Error(), false)
	} else {
		report.add(severityOK, "Windows firewall rule", summarize(output), false)
	}
}

func addRootChecks(report *Report, configuration config.Config) {
	if len(configuration.Server.AppsRoots) == 0 {
		report.add(severityFailure, "Apps folder", "no Apps folder is configured", true)
	}
	for _, root := range configuration.Server.AppsRoots {
		if err := readableDirectory(root); err != nil {
			report.add(severityFailure, "Apps folder", fmt.Sprintf("%s — %v", root, err), true)
		} else {
			report.add(severityOK, "Apps folder", root+" — readable", false)
		}
	}
	for _, registered := range configuration.Server.RegisteredApps {
		if err := readableDirectory(registered); err != nil {
			report.add(severityFailure, "Registered app folder", fmt.Sprintf("%s — %v", registered, err), true)
		} else {
			report.add(severityOK, "Registered app folder", registered+" — readable", false)
		}
	}
}

func readableDirectory(path string) error {
	// #nosec G304 -- paths come from the user's local Dropserve configuration.
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = directory.Close()
	}()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("not a directory")
	}
	_, err = directory.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func addAppChecks(report *Report, result scanner.Result) {
	if len(result.Apps) == 0 {
		report.add(severityWarning, "Apps", "none detected", false)
	}
	for _, application := range result.Apps {
		detail := fmt.Sprintf("%s at %s; detected as %s", application.Kind, application.Path, application.Detection)
		if application.Runtime != "" {
			detail += "; runtime=" + application.Runtime
		}
		report.add(severityOK, "App "+application.Slug, detail, false)
	}
	warningSeverity := severityOK
	if len(result.Warnings) != 0 {
		warningSeverity = severityWarning
	}
	report.add(warningSeverity, "App warnings", fmt.Sprintf("%d", len(result.Warnings)), false)
	for _, warning := range result.Warnings {
		report.add(severityWarning, "App warning", warning.Message, false)
	}
}

func addRuntimeChecks(report *Report, applications []app.App, probes Probes) {
	needed := make(map[string]bool)
	for _, application := range applications {
		needed[application.Runtime] = true
	}
	runtimes := []struct {
		name       string
		candidates []string
	}{
		{name: "node", candidates: []string{"node"}},
		{name: "python", candidates: []string{"python", "python3"}},
		{name: "php", candidates: []string{"php"}},
	}
	for _, item := range runtimes {
		found := ""
		for _, candidate := range item.candidates {
			path, err := probes.LookPath(candidate)
			if err == nil {
				found = path
				break
			}
		}
		if found != "" {
			report.add(severityOK, "Runtime "+item.name, found, false)
			continue
		}
		if needed[item.name] {
			report.add(severityFailure, "Runtime "+item.name, "required by a detected app but not found on PATH", true)
		} else {
			report.add(severityWarning, "Runtime "+item.name, "not found on PATH; no detected app requires it", false)
		}
	}
}

func addMDNSCheck(report *Report, probes Probes) {
	if err := probes.ProbeMDNS(); err != nil {
		report.add(severityWarning, "mDNS bind", "UDP 5353 is unavailable: "+err.Error(), false)
	} else {
		report.add(severityOK, "mDNS bind", "UDP 5353 is available", false)
	}
}

func addTailscaleCheck(report *Report, probes Probes) {
	path, err := probes.LookPath("tailscale")
	if err != nil {
		report.add(severityWarning, "Tailscale", "not installed or not on PATH", false)
		return
	}
	output, err := probes.RunCommand(path, "status")
	if err != nil {
		report.add(severityWarning, "Tailscale", "installed but status failed: "+err.Error(), false)
		return
	}
	report.add(severityOK, "Tailscale", summarize(output), false)
}

func addAutostartCheck(report *Report, probes Probes) {
	enabled, err := probes.AutostartEnabled()
	if err != nil {
		report.add(severityWarning, "Autostart", "OS registration could not be queried: "+err.Error(), false)
		return
	}
	if enabled {
		report.add(severityOK, "Autostart", "enabled according to the operating system", false)
	} else {
		report.add(severityWarning, "Autostart", "disabled according to the operating system", false)
	}
}

func addLogChecks(report *Report, directory string) {
	lines, err := lastErrorLines(directory)
	if err != nil {
		report.add(severityWarning, "Error logs", err.Error(), false)
		return
	}
	report.add(severityOK, "Error logs", fmt.Sprintf("%d error-level line(s), newest 20 follow", len(lines)), false)
	for _, line := range lines {
		report.add(severityWarning, "Error log", line, false)
	}
}

func lastErrorLines(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) || directory == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read log directory: %w", err)
	}
	type logFile struct {
		name     string
		modified time.Time
	}
	files := make([]logFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.Contains(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect log %s: %w", entry.Name(), err)
		}
		files = append(files, logFile{name: entry.Name(), modified: info.ModTime()})
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].modified.Equal(files[right].modified) {
			return files[left].name < files[right].name
		}
		return files[left].modified.Before(files[right].modified)
	})
	lines := make([]string, 0, 20)
	for _, log := range files {
		path := filepath.Join(directory, log.name)
		// #nosec G304 -- directory is Dropserve's machine-owned log directory and names come from ReadDir.
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open log %s: %w", log.name, err)
		}
		content, readErr := io.ReadAll(io.LimitReader(file, 1<<20))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read log %s: %w", log.name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close log %s: %w", log.name, closeErr)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if errorLevelLine(line) {
				lines = append(lines, strings.TrimSpace(line))
				if len(lines) > 20 {
					lines = lines[len(lines)-20:]
				}
			}
		}
	}
	return lines, nil
}

func errorLevelLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"error", "fatal", "panic", "failed", "crash"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func probeMDNS() error {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
	if err != nil {
		return err
	}
	return listener.Close()
}

func runCommand(name string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...) // #nosec G204 -- names and arguments are fixed local diagnostic commands.
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}

func summarize(output []byte) string {
	text := strings.Join(strings.Fields(string(output)), " ")
	if text == "" {
		return "no details returned"
	}
	const limit = 500
	if len(text) > limit {
		return text[:limit] + "…"
	}
	return text
}
