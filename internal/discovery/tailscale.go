package discovery

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// TailscaleStatus is the user-facing result of `tailscale status --json`.
type TailscaleStatus struct {
	BackendState string
	Host         string
	Message      string
	ServeEnabled bool
}

// ParseTailscaleStatus extracts only the local node fields Dropserve needs.
func ParseTailscaleStatus(data []byte) (TailscaleStatus, error) {
	var payload struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return TailscaleStatus{}, fmt.Errorf("decode Tailscale status: %w", err)
	}
	status := TailscaleStatus{BackendState: payload.BackendState}
	switch strings.ToLower(payload.BackendState) {
	case "running":
		status.Host = strings.TrimSuffix(strings.TrimSpace(payload.Self.DNSName), ".")
		if status.Host == "" {
			for _, candidate := range payload.Self.TailscaleIPs {
				if address, err := netip.ParseAddr(candidate); err == nil && !address.IsUnspecified() {
					status.Host = address.String()
					status.Message = "MagicDNS is unavailable, so this uses the Tailscale IP."
					break
				}
			}
		}
		if status.Host == "" {
			status.Message = "Tailscale is running but has no reachable address yet."
		}
	case "stopped":
		status.Message = "Tailscale is stopped. Start it to reach Dropserve through your tailnet."
	case "needslogin":
		status.Message = "Sign in to Tailscale to reach Dropserve through your tailnet."
	default:
		status.Message = "Tailscale is not ready yet."
	}
	return status, nil
}
