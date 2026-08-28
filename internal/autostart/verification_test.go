package autostart

import (
	"errors"
	"strings"
	"testing"
)

func TestEnableVerificationRequiresActualOSRegistration(t *testing.T) {
	if err := verifyEnabled("test registration", func() (bool, error) { return true, nil }); err != nil {
		t.Fatalf("verified registration returned %v", err)
	}
	if err := verifyEnabled("test registration", func() (bool, error) { return false, nil }); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("missing registration error = %v, want an actionable not-enabled error", err)
	}
	probeErr := errors.New("status unavailable")
	if err := verifyEnabled("test registration", func() (bool, error) { return false, probeErr }); !errors.Is(err, probeErr) {
		t.Fatalf("probe error = %v, want wrapped %v", err, probeErr)
	}
}
