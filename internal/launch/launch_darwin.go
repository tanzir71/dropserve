package launch

// OpenURL asks macOS to open address with the user's default browser.
func OpenURL(address string) error {
	return startAndRelease("open", address)
}
