package preferences

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPreferencesPersistPinHideAndLastUseOutsideApps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "dashboard.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open preferences: %v", err)
	}
	pinned := true
	hidden := true
	if err := store.Set("field-notes", &pinned, &hidden); err != nil {
		t.Fatalf("set preferences: %v", err)
	}
	now := time.Date(2026, time.August, 28, 6, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if err := store.Touch("field-notes"); err != nil {
		t.Fatalf("touch app: %v", err)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatalf("reload preferences: %v", err)
	}
	settings := reloaded.Get("field-notes")
	if settings.Pinned == nil || !*settings.Pinned || settings.Hidden == nil || !*settings.Hidden || !settings.LastUsed.Equal(now) {
		t.Fatalf("reloaded preferences = %#v", settings)
	}

	pinned = false
	hidden = false
	if err := reloaded.Set("field-notes", &pinned, &hidden); err != nil {
		t.Fatalf("persist explicit false preferences: %v", err)
	}
	reloaded, err = Open(path)
	if err != nil {
		t.Fatalf("reload explicit false preferences: %v", err)
	}
	settings = reloaded.Get("field-notes")
	if settings.Pinned == nil || *settings.Pinned || settings.Hidden == nil || *settings.Hidden {
		t.Fatalf("explicit false preferences were not preserved: %#v", settings)
	}
}
