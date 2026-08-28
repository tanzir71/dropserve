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

type testMDNSResponder struct{}

func (testMDNSResponder) Shutdown() {}

func TestMDNSStartsWhenLANAppearsAfterOfflineStartup(t *testing.T) {
	var registrations []MDNSRegistration
	manager := NewManager(ManagerOptions{
		RegisterMDNS: func(registration MDNSRegistration) (MDNSResponder, error) {
			registrations = append(registrations, registration)
			return testMDNSResponder{}, nil
		},
		Logf: func(string, ...any) {},
	})
	defer manager.Close()
	manager.StartMDNS()
	if len(registrations) != 0 {
		t.Fatalf("offline startup registered mDNS %d times, want 0", len(registrations))
	}
	address := netip.MustParseAddr("192.168.1.77")
	manager.UpdateLANIP(address)
	if len(registrations) != 1 || len(registrations[0].Addresses) != 1 || registrations[0].Addresses[0] != address.String() {
		t.Fatalf("mDNS registrations after LAN appeared = %#v", registrations)
	}
	if manager.Snapshot().MDNSHostname != "dropserve.local" {
		t.Fatalf("mDNS hostname = %q, want dropserve.local", manager.Snapshot().MDNSHostname)
	}
}
