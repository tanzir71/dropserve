package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// TailscaleFunnelExecutor returns the reversible per-app command boundary used
// by FunnelManager.
func TailscaleFunnelExecutor(mainPort int, probes TailscaleProbes) func(context.Context, FunnelAction) error {
	probes = defaultTailscaleProbes(probes)
	return func(ctx context.Context, action FunnelAction) error {
		if !validAppSlug(action.Slug) {
			return fmt.Errorf("invalid app slug %q", action.Slug)
		}
		binary := locateTailscale(probes)
		if binary == "" {
			return fmt.Errorf("the Tailscale CLI is not installed")
		}
		mount := "--set-path=/" + action.Slug
		arguments := []string{"funnel", "--https=443", mount}
		if action.Enable {
			target := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(mainPort)) + "/" + action.Slug + "/"
			arguments = []string{"funnel", "--bg", "--yes", "--https=443", mount, target}
		} else {
			arguments = append(arguments, "off")
		}
		return runTailscaleSharing(ctx, probes, binary, arguments)
	}
}

// SetTailscaleServe enables or disables tailnet-only HTTPS for all Dropserve
// routes through the installed Tailscale client.
func SetTailscaleServe(ctx context.Context, mainPort int, enable bool, probes TailscaleProbes) error {
	probes = defaultTailscaleProbes(probes)
	binary := locateTailscale(probes)
	if binary == "" {
		return fmt.Errorf("the Tailscale CLI is not installed")
	}
	arguments := []string{"serve", "--https=443", "off"}
	if enable {
		target := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(mainPort))
		arguments = []string{"serve", "--bg", "--yes", "--https=443", target}
	}
	return runTailscaleSharing(ctx, probes, binary, arguments)
}

func runTailscaleSharing(ctx context.Context, probes TailscaleProbes, binary string, arguments []string) error {
	output, err := probes.Run(ctx, binary, arguments...)
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("run tailscale %s: %w", arguments[0], err)
	}
	return fmt.Errorf("run tailscale %s: %w: %s", arguments[0], err, detail)
}

func validAppSlug(slug string) bool {
	if slug == "" {
		return false
	}
	for _, character := range slug {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}
