package main

import (
	"os"
	"strings"
	"testing"
)

func TestFreshMachineSmokeUsesASeparatePinnedWSL2Guest(t *testing.T) {
	t.Parallel()
	script, err := os.ReadFile("release/m10-fresh-machine.ps1") // #nosec G304 -- fixed checked-in smoke script.
	if err != nil {
		t.Fatalf("read fresh-machine smoke: %v", err)
	}
	text := string(script)
	for _, marker := range []string{
		"/VERYSILENT",
		"alpine-minirootfs-3.24.1-x86_64.tar.gz",
		"41f73e3cf5fa919b8aa5ca6b30dc48f0da2720776d7423e2a7748211456fe081",
		"--version 2",
		"wget -T 15 -qO-",
		"M10 fresh-machine transcript",
		"wsl.exe --unregister",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("fresh-machine smoke is missing %q", marker)
		}
	}
	for _, forbidden := range []string{"127.0.0.1:", "localhost:"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("fresh-machine smoke uses same-host endpoint %q", forbidden)
		}
	}
}
