package autostart

import (
	"strings"
	"testing"
)

func TestLaunchAgentRunsInBackgroundAndRestartsOnFailure(t *testing.T) {
	plist := string(makeLaunchAgent(`/Applications/Dropserve & Tools/dropserve`))
	for _, required := range []string{
		`<key>Label</key>`,
		`<string>dev.dropserve.agent</string>`,
		`<key>ProgramArguments</key>`,
		`<string>/Applications/Dropserve &amp; Tools/dropserve</string>`,
		`<string>--background</string>`,
		`<key>RunAtLoad</key>`,
		`<true/>`,
		`<key>KeepAlive</key>`,
		`<key>SuccessfulExit</key>`,
		`<false/>`,
		`<key>ProcessType</key>`,
		`<string>Background</string>`,
	} {
		if !strings.Contains(plist, required) {
			t.Errorf("launch agent does not contain %q:\n%s", required, plist)
		}
	}
}
