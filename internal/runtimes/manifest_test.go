package runtimes

import (
	"runtime"
	"testing"
)

func TestCurrentWindowsAddonManifestPinsAllOptionalEngines(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("Windows pack manifest")
	}
	packs := CurrentAddonPacks()
	if len(packs) != 3 {
		t.Fatalf("current add-on pack count = %d, want 3", len(packs))
	}
	expected := map[string]struct {
		version    string
		sha256     string
		executable string
	}{
		"php":      {"8.3.33", "534399107056313246f424adbbb7937337e40fbbf6aa7bc26287ba9cfd2e4a2a", "php-cgi.exe"},
		"mariadb":  {"11.8.9", "830c46727d9278eae212ae3eca44eeb9e71b2a68704e95f344a64fba7b1963f5", "mariadb-11.8.9-winx64/bin/mariadbd.exe"},
		"postgres": {"18.6", "fbe23da234ee31547bf8a36d29dfd81e82b849df2d2b78d2eecb43d360252f8c", "pgsql/bin/postgres.exe"},
	}
	for _, pack := range packs {
		want, found := expected[pack.Name]
		if !found {
			t.Fatalf("unexpected pack %#v", pack)
		}
		if pack.Version != want.version || pack.SHA256 != want.sha256 || pack.Executable != want.executable || pack.URL == "" {
			t.Errorf("pack %s = %#v, want version=%s sha=%s executable=%s", pack.Name, pack, want.version, want.sha256, want.executable)
		}
	}
}
