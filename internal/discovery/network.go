// Package discovery identifies verified addresses through which Dropserve can
// be reached and manages optional local/remote sharing integrations.
package discovery

import (
	"net/netip"
	"strings"
)

// Adapter is the network surface used by LAN address selection. Production
// probes and deterministic tests both reduce operating-system interfaces to
// this representation.
type Adapter struct {
	Name         string
	Up           bool
	Loopback     bool
	DefaultRoute bool
	Addresses    []netip.Addr
}

// SelectLANIP returns the primary private IPv4 address on a physical adapter.
// A usable default-route adapter wins; otherwise the first usable private
// address is returned as a graceful fallback.
func SelectLANIP(adapters []Adapter) (netip.Addr, bool) {
	var fallback netip.Addr
	for _, adapter := range adapters {
		if !adapter.Up || adapter.Loopback || virtualAdapter(adapter.Name) {
			continue
		}
		for _, address := range adapter.Addresses {
			if !address.Is4() || !address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
				continue
			}
			if adapter.DefaultRoute {
				return address, true
			}
			if !fallback.IsValid() {
				fallback = address
			}
		}
	}
	return fallback, fallback.IsValid()
}

func virtualAdapter(name string) bool {
	normalized := strings.ToLower(name)
	for _, marker := range []string{
		"vethernet",
		"virtualbox",
		"tailscale",
		"hyper-v",
		"hyperv",
		"wsl",
		"docker",
		"vmware",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
