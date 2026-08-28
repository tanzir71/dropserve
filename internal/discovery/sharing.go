package discovery

import (
	"fmt"
	"net"
	"net/netip"
)

// Snapshot is the currently verified address surface. Optional fields remain
// empty unless their integration is healthy and reachable.
type Snapshot struct {
	LANIP        netip.Addr
	MDNSHostname string
	Tailscale    TailscaleStatus
	LANChange    *LANChange
}

// LANChange is a persisted old-to-new address notice.
type LANChange struct {
	OldLANIP string `json:"old_lan_ip"`
	NewLANIP string `json:"new_lan_ip"`
}

// Endpoint is one verified address suitable for display, copying, or QR use.
type Endpoint struct {
	Kind    string
	URL     string
	Message string
}

// Endpoints returns only usable addresses. Loopback is the honest fallback
// when the machine has no selected LAN address.
func (snapshot Snapshot) Endpoints(scheme string, port int) []Endpoint {
	host := "127.0.0.1"
	kind := "loopback"
	if snapshot.LANIP.IsValid() {
		host = snapshot.LANIP.String()
		kind = "lan"
	}
	endpoints := []Endpoint{{Kind: kind, URL: addressURL(scheme, host, port)}}
	if snapshot.MDNSHostname != "" {
		endpoints = append(endpoints, Endpoint{
			Kind: "mdns",
			URL:  addressURL(scheme, snapshot.MDNSHostname, port),
		})
	}
	if snapshot.Tailscale.BackendState != "" {
		endpoint := Endpoint{Kind: "tailscale", Message: snapshot.Tailscale.Message}
		if snapshot.Tailscale.Host != "" {
			if snapshot.Tailscale.ServeEnabled {
				endpoint.URL = addressURL("https", snapshot.Tailscale.Host, 443)
				endpoint.Message = "Tailnet-only HTTPS is active."
			} else {
				endpoint.URL = addressURL(scheme, snapshot.Tailscale.Host, port)
			}
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}

func addressURL(scheme, host string, port int) string {
	if (scheme == "http" && port == 80) || (scheme == "https" && port == 443) {
		return fmt.Sprintf("%s://%s/", scheme, host)
	}
	return fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(host, fmt.Sprintf("%d", port)))
}
