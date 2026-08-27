package launch

// OpenURL asks Windows to open address with the user's default browser.
func OpenURL(address string) error {
	return startAndRelease("rundll32.exe", "url.dll,FileProtocolHandler", address)
}
