package runtimes

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	phpVersionWindowsAMD64 = "8.5.10"
	phpSHA256WindowsAMD64  = "22ec430195984d233eb9e62c637a945bbcda06efca2f392d9d96d62c6acd34f8"
)

// CurrentPHPPack returns Dropserve's pinned official PHP artifact for this platform.
func CurrentPHPPack() (Pack, error) {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		return Pack{
			Name:       "php",
			Version:    phpVersionWindowsAMD64,
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        "https://downloads.php.net/~windows/releases/php-8.5.10-nts-Win32-vs17-x64.zip",
			SHA256:     phpSHA256WindowsAMD64,
			Format:     FormatZIP,
			Executable: "php-cgi.exe",
		}, nil
	}
	return Pack{}, fmt.Errorf("the Dropserve PHP pack is not published for %s/%s yet", runtime.GOOS, runtime.GOARCH)
}

// InstalledExecutable returns the verified manifest location when the pack exists.
func InstalledExecutable(root string, pack Pack) (string, bool, error) {
	path := filepath.Join(root, pack.Name, pack.Version, pack.OS+"-"+pack.Arch, pack.Executable)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return path, false, nil
	}
	if err != nil {
		return path, false, fmt.Errorf("inspect installed %s executable: %w", pack.Name, err)
	}
	if !info.Mode().IsRegular() {
		return path, false, fmt.Errorf("installed %s executable is not a regular file", pack.Name)
	}
	return path, true, nil
}
