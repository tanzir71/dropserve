package main

import (
	"os"
	"strings"
	"testing"
)

func TestCIPerformanceFloorUsesTheHandoverThresholds(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/ci.yml") // #nosec G304 -- fixed checked-in workflow.
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	if !strings.Contains(string(workflow), "go run ./scripts/performance") {
		t.Fatal("CI does not run the M11 performance floor")
	}
	script, err := os.ReadFile("performance/main.go") // #nosec G304 -- fixed checked-in performance script.
	if err != nil {
		t.Fatalf("read performance script: %v", err)
	}
	text := string(script)
	for _, marker := range []string{
		"const fixtureApps = 200",
		"const dashboardTTFBFloor = 100 * time.Millisecond",
		"const appsAPILatencyFloor = 200 * time.Millisecond",
		"const minimumStaticRPS = 500",
		"M11 performance transcript",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("performance script is missing %q", marker)
		}
	}
}
