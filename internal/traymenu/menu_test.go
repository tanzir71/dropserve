package traymenu

import (
	"reflect"
	"testing"
)

func TestMenuContractMatchesHandover(t *testing.T) {
	want := []string{
		"Open Dashboard",
		"Open Apps Folder",
		"Copy LAN Link",
		"Pause Serving",
		"Start at Login",
		"Run Doctor",
		"Quit",
	}
	if got := Labels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Labels() = %#v, want %#v", got, want)
	}
}

func TestUpdateLabelNamesTheAvailableVersion(t *testing.T) {
	if got := UpdateLabel("1.4.0"); got != "View Dropserve 1.4.0 release" {
		t.Fatalf("UpdateLabel() = %q", got)
	}
}

func TestEveryTrayStateHasAWindowsIcon(t *testing.T) {
	for _, state := range []State{Running, Warning, Sharing, Paused} {
		icon := Icon(state)
		if len(icon) < 22 {
			t.Fatalf("Icon(%v) has %d bytes, want an ICO header and image", state, len(icon))
		}
		if icon[0] != 0 || icon[1] != 0 || icon[2] != 1 || icon[3] != 0 || icon[4] != 1 || icon[5] != 0 {
			t.Fatalf("Icon(%v) does not have a one-image ICO header: % x", state, icon[:6])
		}
	}
}
