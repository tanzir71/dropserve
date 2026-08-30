package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsHumanAcceptanceHarnessPreservesExplicitConsentAndExternalState(t *testing.T) {
	t.Parallel()
	script, err := os.ReadFile("acceptance/windows-human.ps1") // #nosec G304 -- fixed checked-in acceptance script.
	if err != nil {
		t.Fatalf("read Windows human-acceptance harness: %v", err)
	}
	text := string(script)
	for _, marker := range []string{
		`[ValidateSet("M7", "M8", "All")]`,
		`Set-StrictMode -Version Latest`,
		`Refusing to replace a pre-existing Tailscale Serve configuration`,
		`dropserve tailscale serve`,
		`dropserve tailscale unserve`,
		`Invoke-WebRequest`,
		`Cert:\CurrentUser\Root`,
		`dropserve trust install`,
		`dropserve trust uninstall`,
		`Type TRUST to continue`,
		`M7 PASS`,
		`M8 PASS`,
		`Evidence transcript`,
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("Windows human-acceptance harness is missing %q", marker)
		}
	}
	for _, forbidden := range []string{
		"certutil -addstore",
		"Import-Certificate",
		"tailscale funnel",
		"-Verb RunAs",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Windows human-acceptance harness contains unsafe substitute %q", forbidden)
		}
	}
}
