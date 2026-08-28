package firstrun

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/config"
)

func TestStateFileAloneControlsFirstRunAndExampleCopy(t *testing.T) {
	sandbox := t.TempDir()
	statePath := filepath.Join(sandbox, "state", "state.json")
	configPath := filepath.Join(sandbox, "config", "config.toml")
	appsRoot := filepath.Join(sandbox, "My Apps")
	opened := make(chan string, 1)
	var autostartCalls atomic.Int32
	options := Options{
		StatePath:       statePath,
		ConfigPath:      configPath,
		DefaultAppsRoot: appsRoot,
		Executable:      filepath.Join(sandbox, "dropserve.exe"),
		OpenBrowser: func(address string) error {
			opened <- address
			return nil
		},
		EnableAutostart: func(executable string) error {
			autostartCalls.Add(1)
			if executable != filepath.Join(sandbox, "dropserve.exe") {
				t.Fatalf("autostart executable = %q", executable)
			}
			return nil
		},
	}

	type outcome struct {
		result Result
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := Run(context.Background(), options)
		finished <- outcome{result: result, err: err}
	}()

	var wizardURL string
	select {
	case wizardURL = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("first-run wizard did not open")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	getRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, wizardURL, nil)
	if err != nil {
		t.Fatalf("create first-run request: %v", err)
	}
	response, err := client.Do(getRequest)
	if err != nil {
		t.Fatalf("get first-run screen: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read first-run screen: %v", err)
	}
	page := string(body)
	for _, required := range []string{
		`name="apps_root"`,
		`name="autostart"`,
		`type="checkbox"`,
		`checked`,
		`type="submit"`,
		`>Start</button>`,
		`checks once a day whether a new release is available; it never installs updates by itself`,
	} {
		if !strings.Contains(page, required) {
			t.Errorf("first-run screen does not contain %q", required)
		}
	}
	if controls := strings.Count(page, "<input") + strings.Count(page, "<button"); controls != 3 {
		t.Fatalf("first-run screen has %d controls, want exactly 3", controls)
	}

	form := url.Values{
		"apps_root": {appsRoot},
		"autostart": {"on"},
	}
	postRequest, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		wizardURL+"start",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		t.Fatalf("create first-run submission: %v", err)
	}
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = client.Do(postRequest)
	if err != nil {
		t.Fatalf("submit first-run screen: %v", err)
	}
	completionPage, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read first-run completion page: %v", readErr)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("submit status = %d, want 200", response.StatusCode)
	}
	if !strings.Contains(string(completionPage), "Dropserve is starting") {
		t.Fatalf("completion page = %q", completionPage)
	}

	var first outcome
	select {
	case first = <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("first-run wizard did not finish")
	}
	if first.err != nil {
		t.Fatalf("first Run: %v", first.err)
	}
	if !first.result.Shown || first.result.AppsRoot != appsRoot || !first.result.Autostart {
		t.Fatalf("first result = %#v", first.result)
	}
	if autostartCalls.Load() != 1 {
		t.Fatalf("autostart calls = %d, want 1", autostartCalls.Load())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state marker was not written: %v", err)
	}
	examplePath := filepath.Join(appsRoot, ExampleDirectory, "index.html")
	// #nosec G304 -- examplePath is inside this test's private temporary directory.
	if data, err := os.ReadFile(examplePath); err != nil || !strings.Contains(string(data), "Welcome to Dropserve") {
		t.Fatalf("example app = %q, %v", data, err)
	}
	configuration, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load first-run config: %v", err)
	}
	if len(configuration.Server.AppsRoots) != 1 || configuration.Server.AppsRoots[0] != appsRoot {
		t.Fatalf("configured roots = %#v, want %q", configuration.Server.AppsRoots, appsRoot)
	}

	if err := os.RemoveAll(filepath.Join(appsRoot, ExampleDirectory)); err != nil {
		t.Fatalf("delete example app: %v", err)
	}
	options.OpenBrowser = func(string) error {
		t.Fatal("second run re-opened the wizard")
		return nil
	}
	second, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.Shown {
		t.Fatalf("second result = %#v, wizard should stay hidden", second)
	}
	if _, err := os.Stat(filepath.Join(appsRoot, ExampleDirectory)); !os.IsNotExist(err) {
		t.Fatalf("deleted example app was recreated: %v", err)
	}
	if autostartCalls.Load() != 1 {
		t.Fatalf("second run changed autostart; calls = %d", autostartCalls.Load())
	}
}
