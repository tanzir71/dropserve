package autostart

import "runtime"

// EnableNote returns optional platform guidance after successful registration.
func EnableNote() string {
	return enableNote(runtime.GOOS)
}

func enableNote(goos string) string {
	if goos != "linux" {
		return ""
	}
	return "For a headless box that should keep serving without an active login, run: loginctl enable-linger $USER"
}
