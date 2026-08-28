package runtimes

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	phpVersionWindowsAMD64      = "8.3.33"
	phpSHA256WindowsAMD64       = "534399107056313246f424adbbb7937337e40fbbf6aa7bc26287ba9cfd2e4a2a"
	mariaDBVersionWindowsAMD64  = "11.8.9"
	mariaDBSHA256WindowsAMD64   = "830c46727d9278eae212ae3eca44eeb9e71b2a68704e95f344a64fba7b1963f5"
	postgresVersionWindowsAMD64 = "18.6"
	postgresSHA256WindowsAMD64  = "fbe23da234ee31547bf8a36d29dfd81e82b849df2d2b78d2eecb43d360252f8c"
)

// CurrentAddonPacks returns the pinned optional packs published for this platform.
func CurrentAddonPacks() []Pack {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return nil
	}
	return []Pack{
		{
			Name: "php", Version: phpVersionWindowsAMD64, OS: runtime.GOOS, Arch: runtime.GOARCH,
			URL:    "https://downloads.php.net/~windows/releases/php-8.3.33-nts-Win32-vs16-x64.zip",
			SHA256: phpSHA256WindowsAMD64, Format: FormatZIP, Executable: "php-cgi.exe",
		},
		{
			Name: "mariadb", Version: mariaDBVersionWindowsAMD64, OS: runtime.GOOS, Arch: runtime.GOARCH,
			URL:    "https://downloads.mariadb.org/rest-api/mariadb/11.8.9/mariadb-11.8.9-winx64.zip",
			SHA256: mariaDBSHA256WindowsAMD64, Format: FormatZIP,
			Executable: "mariadb-11.8.9-winx64/bin/mariadbd.exe",
		},
		{
			Name: "postgres", Version: postgresVersionWindowsAMD64, OS: runtime.GOOS, Arch: runtime.GOARCH,
			URL:    "https://get.enterprisedb.com/postgresql/postgresql-18.6-1-windows-x64-binaries.zip",
			SHA256: postgresSHA256WindowsAMD64, Format: FormatZIP, Executable: "pgsql/bin/postgres.exe",
		},
	}
}

// CurrentPHPPack returns Dropserve's pinned official PHP artifact for this platform.
func CurrentPHPPack() (Pack, error) {
	for _, pack := range CurrentAddonPacks() {
		if pack.Name == "php" {
			return pack, nil
		}
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
