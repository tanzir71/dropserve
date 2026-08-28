package main

import (
	"os"
	"strings"
	"testing"
)

func TestNightlyFuzzRunsBothSecurityTargetsForSixtySeconds(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/fuzz.yml") // #nosec G304 -- fixed checked-in workflow.
	if err != nil {
		t.Fatalf("read fuzz workflow: %v", err)
	}
	text := string(workflow)
	for _, marker := range []string{
		"schedule:",
		"workflow_dispatch:",
		"-fuzz=^FuzzSlugSanitiser$ -fuzztime=60s",
		"-fuzz=^FuzzPathResolver$ -fuzztime=60s",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("nightly fuzz workflow is missing %q", marker)
		}
	}
}
