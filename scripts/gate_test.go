package main

import "testing"

func TestWindowsBuildShipsGUIAndConsoleVariants(t *testing.T) {
	specs := buildSpecs("windows")
	if len(specs) != 2 {
		t.Fatalf("buildSpecs(windows) returned %d outputs, want 2", len(specs))
	}
	if specs[0].name != "dropserve.exe" || specs[0].extraLinkFlags != "-H=windowsgui" {
		t.Fatalf("first Windows output = %#v, want the GUI dropserve.exe", specs[0])
	}
	if specs[1].name != "dropserve-cli.exe" || specs[1].extraLinkFlags != "" {
		t.Fatalf("second Windows output = %#v, want the console dropserve-cli.exe", specs[1])
	}
}

func TestNonWindowsBuildShipsOneConsoleBinary(t *testing.T) {
	specs := buildSpecs("linux")
	if len(specs) != 1 || specs[0].name != "dropserve" || specs[0].extraLinkFlags != "" {
		t.Fatalf("buildSpecs(linux) = %#v, want one console binary", specs)
	}
}
