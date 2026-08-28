package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestREADMEEmbedsRealLightAndDarkDashboardScreenshots(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile("../README.md") // #nosec G304 -- fixed checked-in README.
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	for _, path := range []string{
		"docs/screenshots/dashboard-light.png",
		"docs/screenshots/dashboard-dark.png",
	} {
		if !strings.Contains(string(readme), path) {
			t.Errorf("README does not embed %s", path)
		}
		image, readErr := os.ReadFile("../" + path) // #nosec G304 -- each path is fixed above.
		if readErr != nil {
			t.Errorf("read %s: %v", path, readErr)
			continue
		}
		if !bytes.HasPrefix(image, []byte("\x89PNG\r\n\x1a\n")) || len(image) < 10_000 {
			t.Errorf("%s is not a substantial PNG screenshot", path)
		}
	}
}

func TestREADMEPublishesKeylessReleaseVerificationCommand(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile("../README.md") // #nosec G304 -- fixed checked-in README.
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	text := string(readme)
	for _, marker := range []string{
		"cosign verify-blob",
		"--bundle \"$ARTIFACT.sigstore.json\"",
		"--certificate-identity-regexp",
		"--certificate-oidc-issuer https://token.actions.githubusercontent.com",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("README verification instructions are missing %q", marker)
		}
	}
}
