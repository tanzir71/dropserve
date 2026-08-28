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
		"item.prefers_own_port && ownURL",
	} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("dashboard JavaScript does not contain interaction marker %q", marker)
		}
	}
}

func TestSharingControlsAreWired(t *testing.T) {
	t.Parallel()
	index, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read embedded dashboard HTML: %v", err)
	}
	script, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read embedded dashboard JavaScript: %v", err)
	}
	for _, marker := range []string{`id="funnel-dialog"`, `id="funnel-confirmation"`, "public internet", "eight hours"} {
		if !strings.Contains(string(index), marker) {
			t.Errorf("dashboard HTML does not contain sharing marker %q", marker)
		}
	}
	for _, marker := range []string{
		"/_dropserve/api/sharing/tailscale",
		"/_dropserve/api/sharing/funnel/",
		"Use HTTPS anywhere",
		"Share publicly",
		"Stop public sharing",
		"Show public QR",
		"https://tailscale.com/download",
	} {
		if !strings.Contains(string(script), marker) {
			t.Errorf("dashboard JavaScript does not contain sharing marker %q", marker)
		}
	}
}

func TestLocalHTTPSExplanationAndControlsAreWired(t *testing.T) {
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
		`id="local-https"`,
		"Prefer Tailscale",
		"This adds a certificate authority created on this computer to this computer's trust store",
		"It only affects this machine. You can remove it any time.",
		"/_dropserve/api/https/root.pem",
	} {
		if !strings.Contains(string(index), marker) {
			t.Errorf("dashboard HTML does not contain local HTTPS marker %q", marker)
		}
	}
	for _, marker := range []string{
		"/_dropserve/api/https",
		"/_dropserve/api/trust",
		"Enable local HTTPS",
		"Trust on this computer",
		"Remove local trust",
	} {
		if !strings.Contains(string(script), marker) {
			t.Errorf("dashboard JavaScript does not contain local HTTPS marker %q", marker)
		}
	}
}

func TestDashboardCommandLogSurfaceIsWired(t *testing.T) {
	t.Parallel()

	index, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read embedded dashboard HTML: %v", err)
	}
	script, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read embedded dashboard JavaScript: %v", err)
	}
	styles, err := assets.ReadFile("assets/app.css")
	if err != nil {
		t.Fatalf("read embedded dashboard CSS: %v", err)
	}
	for _, marker := range []string{`id="log-dialog"`, `id="log-output"`, `id="log-refresh"`} {
		if !strings.Contains(string(index), marker) {
			t.Fatalf("dashboard HTML does not contain log marker %q", marker)
		}
	}
	for _, marker := range []string{"/_dropserve/api/logs/", "View logs", "crash-preview", "lastLogLines"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("dashboard JavaScript does not contain log marker %q", marker)
		}
	}
	for _, marker := range []string{`data-status="crashed"`, ".log-output", ".crash-preview"} {
		if !strings.Contains(string(styles), marker) {
			t.Fatalf("dashboard CSS does not contain log marker %q", marker)
		}
	}
}

func TestDashboardOwnPortRescueSurfaceIsWired(t *testing.T) {
	t.Parallel()

	script, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read embedded dashboard JavaScript: %v", err)
	}
	styles, err := assets.ReadFile("assets/app.css")
	if err != nil {
		t.Fatalf("read embedded dashboard CSS: %v", err)
	}
	for _, marker := range []string{
		"This app expects to live at the root",
		"Open on its own port",
		"Use the short URL anyway",
		"open-own",
		"open-path",
	} {
		if !strings.Contains(string(script), marker) {
			t.Errorf("dashboard JavaScript does not contain own-port marker %q", marker)
		}
	}
	if !strings.Contains(string(styles), ".own-port-note") {
		t.Error("dashboard CSS does not style the own-port explanation")
	}
}
