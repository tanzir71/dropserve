package autostart

import (
	"strings"
	"testing"
)

func TestSystemdUnitRunsInBackgroundAndRestartsOnFailure(t *testing.T) {
	t.Parallel()

	unit := string(makeSystemdUnit(`/opt/Drop Serve/dropserve%stable`))
	wants := []string{
		"[Unit]\n",
		"After=network-online.target\n",
		"[Service]\n",
		`ExecStart="/opt/Drop Serve/dropserve%%stable" --background`,
		"Restart=on-failure\n",
		"RestartSec=60s\n",
		"[Install]\n",
		"WantedBy=default.target\n",
	}
	for _, want := range wants {
		if !strings.Contains(unit, want) {
			t.Errorf("systemd unit does not contain %q:\n%s", want, unit)
		}
	}
}
