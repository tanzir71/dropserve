//go:build !windows && !darwin

package launch

// OpenURL asks a freedesktop-compatible desktop to open address.
func OpenURL(address string) error {
	return startAndRelease("xdg-open", address)
}
