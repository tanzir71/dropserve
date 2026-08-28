package app

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCommandDetectionRulesTwoFourAndEight(t *testing.T) {
	t.Parallel()
	executableName := "utility-bin"
	if runtime.GOOS == "windows" {
		executableName = "utility.exe"
	}

	tests := []struct {
		name        string
		files       map[string]string
		wantCommand func(string) []string
		wantReason  string
		wantRuntime string
	}{
		{
			name:  "Procfile web command",
			files: map[string]string{"Procfile": "release: ignored\nweb: node server.js --demo\n"},
			wantCommand: func(string) []string {
				return []string{"node", "server.js", "--demo"}
			},
			wantReason:  "Command app from Procfile web entry",
			wantRuntime: "node",
		},
		{
			name: "package main",
			files: map[string]string{
				"package.json": `{"name":"main-app","main":"src/app.js"}`,
				"src/app.js":   "console.log('main')",
			},
			wantCommand: func(string) []string {
				return []string{"node", filepath.FromSlash("src/app.js")}
			},
			wantReason:  "Node app from package.json main entry",
			wantRuntime: "node",
		},
		{
			name: "package server fallback",
			files: map[string]string{
				"package.json": `{"name":"server-app"}`,
				"server.js":    "console.log('server')",
			},
			wantCommand: func(string) []string {
				return []string{"node", "server.js"}
			},
			wantReason:  "Node app from server.js",
			wantRuntime: "node",
		},
		{
			name:  "single platform executable",
			files: map[string]string{executableName: "fixture"},
			wantCommand: func(root string) []string {
				return []string{filepath.Join(root, executableName)}
			},
			wantReason: "Command app from single executable " + executableName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, content := range test.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatalf("create fixture directory: %v", err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatalf("write fixture %s: %v", name, err)
				}
				if name == executableName {
					if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- the fixture must be executable to cover rule 8.
						t.Fatalf("make fixture executable: %v", err)
					}
				}
			}
			detection, err := Detect(root)
			if err != nil {
				t.Fatalf("detect fixture: %v", err)
			}
			if detection.Kind != KindCommand || !reflect.DeepEqual(detection.Command, test.wantCommand(root)) {
				t.Fatalf("detection = %#v", detection)
			}
			if detection.Reason != test.wantReason || detection.Runtime != test.wantRuntime {
				t.Fatalf("detection metadata = %#v", detection)
			}
		})
	}
}

func TestMalformedManifestWarnsAndFallsBackToAutomaticDetection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dropserve.json"), []byte(`{"type":`), 0o600); err != nil {
		t.Fatalf("write malformed manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.py"), []byte("print('ready')"), 0o600); err != nil {
		t.Fatalf("write automatic fixture: %v", err)
	}
	detection, err := Detect(root)
	if err != nil {
		t.Fatalf("malformed optional manifest aborted detection: %v", err)
	}
	if detection.Kind != KindCommand || !reflect.DeepEqual(detection.Command, []string{"python", "server.py"}) {
		t.Fatalf("fallback detection = %#v", detection)
	}
	if len(detection.Warnings) != 1 || detection.Warnings[0].Code != "manifest_parse" || !strings.Contains(detection.Warnings[0].Message, "automatic detection") {
		t.Fatalf("manifest warnings = %#v", detection.Warnings)
	}
}

func TestManifestOverridesCommandSettingsAndWarnsAboutUnknownKeys(t *testing.T) {
	root := t.TempDir()
	manifest := `{
		"name":"Invoice Maker","description":"Makes invoices","icon":"📄","tags":["work","pdf","work"],
		"type":"command","command":"node \"custom server.js\" --label='hello world'","port_env":"APP_PORT","env":{"NODE_ENV":"production"},
		"health_path":"/ready","autostart":false,"base_href":"never","visibility":"tailnet","pinned":true,"hidden":true,
		"future_option":true
	}`
	if err := os.WriteFile(filepath.Join(root, "dropserve.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	detection, err := Detect(root)
	if err != nil {
		t.Fatalf("detect manifest app: %v", err)
	}
	if detection.Kind != KindCommand || !reflect.DeepEqual(detection.Command, []string{"node", "custom server.js", "--label=hello world"}) || detection.PortEnv != "APP_PORT" || detection.HealthPath != "/ready" {
		t.Fatalf("command settings = %#v", detection)
	}
	if detection.Name != "Invoice Maker" || detection.Description != "Makes invoices" || detection.Icon != "📄" || !reflect.DeepEqual(detection.Tags, []string{"work", "pdf"}) {
		t.Fatalf("display settings = %#v", detection)
	}
	if detection.Autostart || detection.BaseHref != "never" || detection.Visibility != "tailnet" || !detection.Pinned || !detection.Hidden || detection.Environment["NODE_ENV"] != "production" {
		t.Fatalf("runtime settings = %#v", detection)
	}
	if len(detection.Warnings) != 1 || detection.Warnings[0].Code != "manifest_unknown_key" || !strings.Contains(detection.Warnings[0].Message, "future_option") {
		t.Fatalf("manifest warnings = %#v", detection.Warnings)
	}
}

func TestManifestTypeOverrideDoesNotParseIrrelevantBrokenPackageMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dropserve.json"), []byte(`{"type":"static"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":`), 0o600); err != nil {
		t.Fatal(err)
	}
	detection, err := Detect(root)
	if err != nil {
		t.Fatalf("static type override parsed irrelevant package.json: %v", err)
	}
	if detection.Kind != KindStatic || detection.Reason != "Static app from dropserve.json type override" {
		t.Fatalf("type override detection = %#v", detection)
	}
}
