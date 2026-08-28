package autostart

import (
	"strings"
	"testing"
)

func TestLinuxAutostartExplainsHeadlessLingerOption(t *testing.T) {
	note := enableNote("linux")
	if !strings.Contains(note, "loginctl enable-linger $USER") || !strings.Contains(note, "without an active login") {
		t.Fatalf("Linux enable note = %q", note)
	}
	if note := enableNote("windows"); note != "" {
		t.Fatalf("Windows enable note = %q, want empty", note)
	}
}
