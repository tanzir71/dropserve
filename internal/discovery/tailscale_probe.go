package discovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// TailscaleProbes makes CLI discovery deterministic in tests.
type TailscaleProbes struct {
	GOOS     string
	LookPath func(string) (string, error)
	Exists   func(string) bool
	Run      func(context.Context, string, ...string) ([]byte, error)
}

// ProbeTailscale locates the user's existing client and reads its real status.
// Absence is a normal explained state, not an application error.
func ProbeTailscale(ctx context.Context, probes TailscaleProbes) (TailscaleStatus, error) {
	probes = defaultTailscaleProbes(probes)
	binary := locateTailscale(probes)
	if binary == "" {
		return TailscaleStatus{
			BackendState: "NotInstalled",
			Message:      "Tailscale is not installed. Install it to reach Dropserve through your tailnet.",
		}, nil
	}
	output, err := probes.Run(ctx, binary, "status", "--json")
	if err != nil {
		return TailscaleStatus{}, fmt.Errorf("read Tailscale status: %w", err)
	}
	return ParseTailscaleStatus(output)
}

func locateTailscale(probes TailscaleProbes) string {
	binary, err := probes.LookPath("tailscale")
	if err == nil && binary != "" {
		return binary
	}
	for _, candidate := range tailscaleCandidates(probes.GOOS) {
		if probes.Exists(candidate) {
			return candidate
		}
	}
	return ""
}

func defaultTailscaleProbes(probes TailscaleProbes) TailscaleProbes {
	if probes.GOOS == "" {
		probes.GOOS = runtime.GOOS
	}
	if probes.LookPath == nil {
		probes.LookPath = exec.LookPath
	}
	if probes.Exists == nil {
		probes.Exists = func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && !info.IsDir()
		}
	}
	if probes.Run == nil {
		probes.Run = func(parent context.Context, path string, arguments ...string) ([]byte, error) {
			commandContext, cancel := context.WithTimeout(parent, 10*time.Second)
			defer cancel()
			// #nosec G204 -- path is resolved from PATH or a fixed official Tailscale install location.
			return exec.CommandContext(commandContext, path, arguments...).CombinedOutput()
		}
	}
	return probes
}

func tailscaleCandidates(goos string) []string {
	switch goos {
	case "windows":
		return []string{`C:\Program Files\Tailscale\tailscale.exe`}
	case "darwin":
		return []string{"/Applications/Tailscale.app/Contents/MacOS/Tailscale"}
	case "linux":
		return []string{"/usr/bin/tailscale"}
	default:
		return nil
	}
}
