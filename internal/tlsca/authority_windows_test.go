//go:build windows

package tlsca

import (
	"path/filepath"
	"testing"
	"unsafe"

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
	var grant *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &grant); err != nil {
		t.Fatalf("read root-key access grant: %v", err)
	}
	// #nosec G103 -- ACCESS_ALLOWED_ACE stores its variable-length SID at SidStart by Windows API contract.
	grantee := (*windows.SID)(unsafe.Pointer(&grant.SidStart))
	const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	if grant.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
		grant.Mask != fileAllAccess ||
		!grantee.Equals(user.User.Sid) {
		t.Fatalf(
			"root-key grant = type %d mask %#x SID %s, want full control for current user %s",
			grant.Header.AceType,
			grant.Mask,
			grantee,
			user.User.Sid,
		)
	}
}
