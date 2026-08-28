package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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
