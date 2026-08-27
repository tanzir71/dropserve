package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(version) returned %d; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); !strings.HasPrefix(got, "dropserve ") || !strings.Contains(got, "(") {
		t.Fatalf("version output %q does not contain the product, version, and commit", got)
	}
}

func TestUnknownCommandNamesTheFix(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(nope) returned %d, want 2", code)
	}
	if got := stderr.String(); !strings.Contains(got, "dropserve help") {
		t.Fatalf("error %q does not name the recovery command", got)
	}
}
