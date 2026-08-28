//go:build !windows && !darwin

package launch

// OpenURL asks a freedesktop-compatible desktop to open address.
func OpenURL(address string) error {
	return startAndRelease("xdg-open", address)
}

// OpenPath asks the desktop environment to show path.
func OpenPath(path string) error {
	return startAndRelease("xdg-open", path)
}
