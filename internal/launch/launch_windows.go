package launch

// OpenURL asks Windows to open address with the user's default browser.
func OpenURL(address string) error {
	return startAndRelease("rundll32.exe", "url.dll,FileProtocolHandler", address)
}

// OpenPath asks Explorer to show a local file or folder.
func OpenPath(path string) error {
	return startAndRelease("explorer.exe", path)
}
