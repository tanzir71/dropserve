package app

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
