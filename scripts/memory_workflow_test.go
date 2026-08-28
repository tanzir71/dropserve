package main

import (
	"os"
	"strings"
	"testing"
)

func TestScheduledMemorySoakUsesTheHandoverFloor(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/memory.yml") // #nosec G304 -- fixed checked-in workflow.
	if err != nil {
		t.Fatalf("read memory workflow: %v", err)
	}
	for _, marker := range []string{"schedule:", "workflow_dispatch:", "go run ./scripts/memory -soak=5m"} {
		if !strings.Contains(string(workflow), marker) {
			t.Errorf("memory workflow is missing %q", marker)
		}
	}
	script, err := os.ReadFile("memory/main.go") // #nosec G304 -- fixed checked-in memory script.
	if err != nil {
		t.Fatalf("read memory script: %v", err)
	}
	text := string(script)
	for _, marker := range []string{
		"const fixtureApps = 50",
		"const memoryLimitBytes = 60 * 1024 * 1024",
		"const defaultSoakDuration = 5 * time.Minute",
		"M11 memory transcript",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("memory script is missing %q", marker)
		}
	}
}
