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

func TestWindowsUninstallerRemovesOnlyDropserveOwnedData(t *testing.T) {
	t.Parallel()
	installer, err := os.ReadFile("../packaging/windows/dropserve.iss") // #nosec G304 -- fixed checked-in installer definition.
	if err != nil {
		t.Fatalf("read Windows installer definition: %v", err)
	}
	text := string(installer)
	for _, marker := range []string{
		"[UninstallDelete]",
		`Type: filesandordirs; Name: "{localappdata}\Dropserve"`,
		`Type: filesandordirs; Name: "{userappdata}\Dropserve"`,
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("Windows uninstaller is missing %q", marker)
		}
	}
	uninstallDelete := text
	if index := strings.Index(text, "[UninstallDelete]"); index >= 0 {
		uninstallDelete = text[index:]
	}
	for _, line := range strings.Split(uninstallDelete, "\n") {
		entry := strings.TrimSpace(line)
		if !strings.HasPrefix(entry, "Type:") {
			continue
		}
		for _, forbidden := range []string{"{userprofile}", `\Apps`} {
			if strings.Contains(entry, forbidden) {
				t.Errorf("Windows uninstaller may delete user-owned app data via %q in %q", forbidden, entry)
			}
		}
	}

	smoke, err := os.ReadFile("release/m10-installer.ps1") // #nosec G304 -- fixed checked-in installer smoke.
	if err != nil {
		t.Fatalf("read Windows installer smoke: %v", err)
	}
	smokeText := string(smoke)
	for _, marker := range []string{
		"ownedLocalDataDirectory",
		"ownedRoamingDataDirectory",
		"installer smoke requires a clean runner",
		"uninstaller left Dropserve-owned data",
		"uninstaller removed the user's Apps folder or its contents",
	} {
		if !strings.Contains(smokeText, marker) {
			t.Errorf("Windows installer smoke is missing %q", marker)
		}
	}
}
