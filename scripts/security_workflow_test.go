package main

import (
	"os"
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
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("CI vulnerability gate is missing %q", marker)
		}
	}
}
