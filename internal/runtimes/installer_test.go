package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPackMissingDeclaredExecutableIsNotRegistered(t *testing.T) {
	payload := runtimeZIP(t, "README.txt", "no executable here")
	hash := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(payload)
	}))
	defer server.Close()
	root := t.TempDir()
	_, err := (Installer{Root: root, Client: server.Client()}).Install(context.Background(), Pack{
		Name: "php", Version: "broken", OS: "windows", Arch: "amd64", URL: server.URL,
		SHA256: fmt.Sprintf("%x", hash), Format: FormatZIP, Executable: "php-cgi.exe",
	})
	if err == nil || !strings.Contains(err.Error(), "php-cgi.exe") {
		t.Fatalf("pack without executable error = %v", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("pack without executable was registered: %v", entries)
	}
}

func TestTamperedPackIsRejectedDeletedAndReportedClearly(t *testing.T) {
	official := []byte("official runtime archive")
	tampered := []byte("tampered runtime archive")
	expected := sha256.Sum256(official)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Length", "24")
		_, _ = response.Write(tampered)
	}))
	defer server.Close()
	root := t.TempDir()
	installer := Installer{Root: root, Client: server.Client()}

	_, err := installer.Install(context.Background(), Pack{
		Name:    "php",
		Version: "8.4-test",
		OS:      "windows",
		Arch:    "amd64",
		URL:     server.URL + "/php.zip",
		SHA256:  hex.EncodeToString(expected[:]),
		Format:  FormatZIP,
	})
	if err == nil {
		t.Fatal("tampered pack was accepted")
	}
	for _, marker := range []string{"php", "SHA-256", hex.EncodeToString(expected[:])} {
		if !strings.Contains(err.Error(), marker) {
			t.Errorf("tamper error %q does not contain %q", err, marker)
		}
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("read runtime root: %v", readErr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("tampered download was not deleted; runtime root contains %v", names)
	}
}

func TestRemovingPHPPackLeavesAppFixtureByteIdentical(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "php"))
	if err != nil {
		t.Fatalf("resolve PHP fixture: %v", err)
	}
	before := runtimeFixtureSnapshot(t, fixture)

	root := filepath.Join(t.TempDir(), "runtimes")
	pack := Pack{
		Name: "php", Version: "8.5-test", OS: "windows", Arch: "amd64",
		URL: "https://example.invalid/php.zip", SHA256: strings.Repeat("0", 64),
		Format: FormatZIP, Executable: "php-cgi.exe",
	}
	destination := filepath.Join(root, pack.Name, pack.Version, pack.OS+"-"+pack.Arch)
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatalf("create installed PHP pack: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, pack.Executable), []byte("runtime only"), 0o600); err != nil {
		t.Fatalf("write installed PHP executable: %v", err)
	}
	otherVersion := filepath.Join(root, pack.Name, "8.4-keep", pack.OS+"-"+pack.Arch, pack.Executable)
	if err := os.MkdirAll(filepath.Dir(otherVersion), 0o700); err != nil {
		t.Fatalf("create other PHP pack: %v", err)
	}
	if err := os.WriteFile(otherVersion, []byte("keep this version"), 0o600); err != nil {
		t.Fatalf("write other PHP pack: %v", err)
	}

	if err := (Installer{Root: root}).Remove(pack); err != nil {
		t.Fatalf("remove PHP pack: %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("removed PHP directory still exists: %v", err)
	}
	if content, err := os.ReadFile(otherVersion); err != nil || string(content) != "keep this version" { // #nosec G304 -- path is inside this test's temporary runtime root.
		t.Fatalf("other PHP version changed: content=%q err=%v", content, err)
	}
	after := runtimeFixtureSnapshot(t, fixture)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("PHP app fixture changed during pack removal\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func runtimeFixtureSnapshot(t *testing.T, root string) map[string][sha256.Size]byte {
	t.Helper()
	snapshot := make(map[string][sha256.Size]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path) // #nosec G304,G122 -- WalkDir confines paths to the checked-in PHP fixture with no symlinks.
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = sha256.Sum256(content)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot PHP fixture: %v", err)
	}
	return snapshot
}
