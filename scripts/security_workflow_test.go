package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIRejectsReachableAndModuleOnlyGoVulnerabilities(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/ci.yml") // #nosec G304 -- fixed checked-in workflow.
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	text := string(workflow)
	for _, marker := range []string{
		"name: govulncheck",
		"golang.org/x/vuln/cmd/govulncheck@v1.7.0 -show verbose ./...",
		"grep -q '^Vulnerability #'",
		"name: gosec",
		"github.com/securego/gosec/v2/cmd/gosec@v2.29.0 ./...",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("CI vulnerability gate is missing %q", marker)
		}
	}
}

func TestEveryGosecSuppressionHasAOneLineJustification(t *testing.T) {
	t.Parallel()
	suppressionMarker := "#" + "nosec"
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "dist") {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		content, readErr := os.ReadFile(path) // #nosec G304,G122 -- paths come from WalkDir beneath the checked-out repository.
		if readErr != nil {
			return readErr
		}
		for index, line := range strings.Split(string(content), "\n") {
			if strings.Contains(line, suppressionMarker) {
				parts := strings.SplitN(line, "--", 2)
				if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
					t.Errorf("%s:%d has a gosec suppression without a one-line justification", path, index+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan gosec suppressions: %v", err)
	}
}
