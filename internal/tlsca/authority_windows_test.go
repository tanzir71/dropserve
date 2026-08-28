//go:build windows

package tlsca

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsRootKeyACLGrantsOnlyCurrentUser(t *testing.T) {
	authority, err := New(Options{Directory: filepath.Join(t.TempDir(), "ca"), Hostname: "darkhorse"})
	if err != nil {
		t.Fatalf("create authority: %v", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		authority.RootKeyPath(),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read root-key ACL: %v", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatalf("read root-key DACL: %v", err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("root-key DACL ACE count = %v, want exactly one current-user grant", dacl)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user SID: %v", err)
	}
	if security := descriptor.String(); !strings.Contains(security, user.User.Sid.String()) {
		t.Fatalf("root-key security descriptor %q does not name current user %s", security, user.User.Sid)
	}
}
