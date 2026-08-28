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

type trackedMDNSResponder struct{ stopped *int }

func (responder trackedMDNSResponder) Shutdown() { *responder.stopped++ }

func TestConfigureMDNSRenamesAndDisablesLiveResponder(t *testing.T) {
	var registrations []MDNSRegistration
	stopped := 0
	manager := NewManager(ManagerOptions{
		LANIP: netip.MustParseAddr("192.168.1.10"),
		RegisterMDNS: func(registration MDNSRegistration) (MDNSResponder, error) {
			registrations = append(registrations, registration)
			return trackedMDNSResponder{stopped: &stopped}, nil
		},
	})
	manager.StartMDNS()
	manager.ConfigureMDNS(true, "my-apps")
	if len(registrations) != 2 || registrations[1].Hostname != "my-apps.local." || stopped != 1 || manager.Snapshot().MDNSHostname != "my-apps.local" {
		t.Fatalf("renamed registrations=%#v stopped=%d snapshot=%#v", registrations, stopped, manager.Snapshot())
	}
	manager.ConfigureMDNS(false, "my-apps")
	if stopped != 2 || manager.Snapshot().MDNSHostname != "" {
		t.Fatalf("disabled stopped=%d snapshot=%#v", stopped, manager.Snapshot())
	}
}

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
