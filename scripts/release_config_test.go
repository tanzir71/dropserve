package main

import (
	"os"
	"strings"
	"testing"
)

func TestGoReleaserProducesLinuxPackagesAndSBOMs(t *testing.T) {
	t.Parallel()
	config, err := os.ReadFile("../.goreleaser.yaml") // #nosec G304 -- fixed checked-in release config.
	if err != nil {
		t.Fatalf("read GoReleaser config: %v", err)
	}
	text := string(config)
	for _, marker := range []string{
		"nfpms:",
		"formats:\n      - deb\n      - rpm",
		"sboms:",
		"artifacts: archive",
		"artifacts: package",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("GoReleaser config is missing %q", marker)
		}
	}
}

func TestReleaseWorkflowSignsEveryArtifactKeylessly(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/release.yml") // #nosec G304 -- fixed checked-in workflow.
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(workflow)
	for _, marker := range []string{
		"cosign-installer",
		"cosign sign-blob",
		"--yes",
		"*.sbom.json",
		"*.deb",
		"*.rpm",
		"@($installer.FullName, $sbom)",
		"Get-FileHash -LiteralPath $artifact",
		"checksums.txt.sigstore.json",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("release workflow is missing %q", marker)
		}
	}
}
