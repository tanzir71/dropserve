package launch

// OpenURL asks macOS to open address with the user's default browser.
func OpenURL(address string) error {
	return startAndRelease("open", address)
}

// OpenPath asks Finder or the default application to show path.
func OpenPath(path string) error {
	return startAndRelease("open", path)
}
