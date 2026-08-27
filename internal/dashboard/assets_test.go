package dashboard

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedAssetsStayUnderBudget(t *testing.T) {
	t.Parallel()

	const maximumBytes = 100_000
	var total int64
	err := fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		t.Logf("%s: %d bytes", path, info.Size())
		return nil
	})
	if err != nil {
		t.Fatalf("measure embedded dashboard assets: %v", err)
	}
	if total >= maximumBytes {
		t.Fatalf("embedded dashboard assets total %d bytes, must stay below %d", total, maximumBytes)
	}
	t.Logf("dashboard asset total: %d/%d bytes", total, maximumBytes)
}

func TestDashboardInteractionSurfaceIsWired(t *testing.T) {
	t.Parallel()

	index, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read embedded dashboard HTML: %v", err)
	}
	script, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read embedded dashboard JavaScript: %v", err)
	}
	for _, marker := range []string{
		`id="sharing-toggle"`,
		`id="sharing-panel"`,
		`id="qr-dialog"`,
		`aria-controls="sharing-panel"`,
	} {
		if !strings.Contains(string(index), marker) {
			t.Fatalf("dashboard HTML does not contain interaction marker %q", marker)
		}
	}
	for _, marker := range []string{
		"/_dropserve/api/urls",
		"/_dropserve/api/qr?url=",
		"/_dropserve/api/events",
		"/_dropserve/api/status",
		"new EventSource",
		"navigator.clipboard",
		"showModal",
		"data-action",
	} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("dashboard JavaScript does not contain interaction marker %q", marker)
		}
	}
}
