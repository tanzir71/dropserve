package main

import (
	"os"
	"strings"
	"testing"
)

func TestDashboardAccessibilityRunsRenderedChecksInBothThemes(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/accessibility.yml") // #nosec G304 -- fixed checked-in workflow.
	if err != nil {
		t.Fatalf("read accessibility workflow: %v", err)
	}
	for _, marker := range []string{"workflow_dispatch:", "npx playwright install --with-deps chromium", "npm test"} {
		if !strings.Contains(string(workflow), marker) {
			t.Errorf("accessibility workflow is missing %q", marker)
		}
	}

	spec, err := os.ReadFile("accessibility/dashboard.spec.js") // #nosec G304 -- fixed checked-in browser spec.
	if err != nil {
		t.Fatalf("read accessibility browser spec: %v", err)
	}
	for _, marker := range []string{
		"AxeBuilder",
		"wcag2aa",
		"light",
		"dark",
		"keyboard.press('Tab')",
		"toHaveAccessibleName",
	} {
		if !strings.Contains(string(spec), marker) {
			t.Errorf("accessibility browser spec is missing %q", marker)
		}
	}

	styles, err := os.ReadFile("../internal/dashboard/assets/app.css") // #nosec G304 -- fixed checked-in stylesheet.
	if err != nil {
		t.Fatalf("read dashboard stylesheet: %v", err)
	}
	if !strings.Contains(string(styles), ":focus-visible") {
		t.Error("dashboard stylesheet has no explicit :focus-visible treatment")
	}
}
