package doctor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/state"
)

func TestHealthyReportContainsEverySupportCheck(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	appsRoot := filepath.Join(sandbox, "Apps")
	appRoot := filepath.Join(appsRoot, "status-page")
	if err := os.MkdirAll(appRoot, 0o750); err != nil {
		t.Fatalf("create app root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appRoot, "index.html"), []byte("<h1>Status</h1>"), 0o600); err != nil {
		t.Fatalf("write static app: %v", err)
	}
	configuration := config.Default()
	configuration.Server.AppsRoots = []string{appsRoot}
	configuration.Server.HTTPPort = 80
	configPath := filepath.Join(sandbox, "config.toml")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save config: %v", err)
	}
	statePath := filepath.Join(sandbox, "state.json")
	if err := state.Save(statePath, state.State{HTTPPort: 80}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	logDirectory := filepath.Join(sandbox, "logs")
	if err := os.Mkdir(logDirectory, 0o700); err != nil {
		t.Fatalf("create log directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDirectory, "status-page.log"), []byte("ready\nERROR example failure\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	report := Diagnose(Options{
		ConfigPath:   configPath,
		StatePath:    statePath,
		LogDirectory: logDirectory,
		Version:      "1.2.3",
		Commit:       "abc1234",
	}, Probes{
		OS: "linux",
		LookPath: func(name string) (string, error) {
			return "/usr/bin/" + name, nil
		},
		RunCommand: func(_ string, _ ...string) ([]byte, error) {
			return []byte("healthy"), nil
		},
		ProbeMDNS: func() error { return nil },
		AutostartEnabled: func() (bool, error) {
			return true, nil
		},
	})
	if report.RequiredFailure() {
		t.Fatal("healthy report contains a required failure")
	}
	var output bytes.Buffer
	if err := report.Write(&output); err != nil {
		t.Fatalf("write report: %v", err)
	}
	text := output.String()
	for _, label := range []string{
		"Version:",
		"HTTP port:",
		"Windows excluded TCP port ranges:",
		"Windows firewall rule:",
		"Apps root:",
		"App status-page:",
		"App warnings:",
		"Runtime node:",
		"Runtime python:",
		"Runtime php:",
		"mDNS bind:",
		"Tailscale:",
		"Autostart:",
		"Error logs:",
	} {
		if !strings.Contains(text, label) {
			t.Errorf("doctor output does not contain %q:\n%s", label, text)
		}
	}
	if !strings.Contains(text, "ERROR example failure") {
		t.Fatalf("doctor output omits the error-level log line:\n%s", text)
	}
}

func TestUnreadableRequiredRootFailsReport(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	configuration := config.Default()
	configuration.Server.AppsRoots = []string{filepath.Join(sandbox, "missing-root")}
	configPath := filepath.Join(sandbox, "config.toml")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save config: %v", err)
	}

	report := Diagnose(Options{
		ConfigPath: configPath,
		StatePath:  filepath.Join(sandbox, "state.json"),
		Version:    "test",
		Commit:     "test",
	}, Probes{
		OS: "linux",
		LookPath: func(string) (string, error) {
			return "", errors.New("not installed")
		},
		ProbeMDNS:        func() error { return nil },
		RunCommand:       func(string, ...string) ([]byte, error) { return nil, nil },
		AutostartEnabled: func() (bool, error) { return false, nil },
	})
	if !report.RequiredFailure() {
		t.Fatal("missing required Apps root did not fail the report")
	}
	var output bytes.Buffer
	if err := report.Write(&output); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if text := output.String(); !strings.Contains(text, "❌ Apps root:") || !strings.Contains(text, "missing-root") {
		t.Fatalf("missing root is not explained:\n%s", text)
	}
}

func TestErrorLogsReturnOnlyNewestTwentyLines(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	var content strings.Builder
	for index := range 25 {
		content.WriteString("ERROR line ")
		content.WriteString(strconv.Itoa(index))
		content.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(directory, "sample.log"), []byte(content.String()), 0o600); err != nil {
		t.Fatalf("write error log: %v", err)
	}
	lines, err := lastErrorLines(directory)
	if err != nil {
		t.Fatalf("read error logs: %v", err)
	}
	if len(lines) != 20 || lines[0] != "ERROR line 5" || lines[19] != "ERROR line 24" {
		t.Fatalf("error lines = %#v, want lines 5 through 24", lines)
	}
}
