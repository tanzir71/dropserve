package discovery

import (
	"net/netip"
	"testing"
)

func TestVirtualAdaptersAreFilteredFromLANSelection(t *testing.T) {
	adapters := []Adapter{
		{Name: "vEthernet (WSL)", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("172.20.0.1")}},
		{Name: "Tailscale", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("100.100.20.30")}},
		{Name: "VirtualBox Host-Only Network", Up: true, Addresses: []netip.Addr{netip.MustParseAddr("192.168.56.1")}},
		{Name: "Ethernet", Up: true, DefaultRoute: true, Addresses: []netip.Addr{netip.MustParseAddr("192.168.68.110")}},
	}

	got, ok := SelectLANIP(adapters)
	if !ok {
		t.Fatal("SelectLANIP() found no address, want the physical Ethernet address")
	}
	want := netip.MustParseAddr("192.168.68.110")
	if got != want {
		t.Fatalf("SelectLANIP() = %s, want %s", got, want)
	}
}
