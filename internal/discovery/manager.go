package discovery

import (
	"log"
	"net/netip"
	"strings"
	"sync"

	"github.com/libp2p/zeroconf/v2"
)

// MDNSResponder is the lifecycle surface returned by the selected zeroconf
// implementation.
type MDNSResponder interface {
	Shutdown()
}

// MDNSRegistration contains the verified values advertised by mDNS.
type MDNSRegistration struct {
	Hostname  string
	Port      int
	Addresses []string
}

// ManagerOptions supplies initial network state and testable integration
// boundaries.
type ManagerOptions struct {
	LANIP        netip.Addr
	HTTPPort     int
	MDNSHostname string
	NoticePath   string
	Logf         func(string, ...any)
	RegisterMDNS func(MDNSRegistration) (MDNSResponder, error)
}

// Manager owns the live, verified discovery snapshot.
type Manager struct {
	mu            sync.RWMutex
	snapshot      Snapshot
	httpPort      int
	mdnsHostname  string
	logf          func(string, ...any)
	registerMDNS  func(MDNSRegistration) (MDNSResponder, error)
	responder     MDNSResponder
	mdnsRequested bool
	closed        bool
	noticePath    string
	lastLANIP     string
}

// NewManager creates a discovery manager without starting optional network
// integrations.
func NewManager(options ManagerOptions) *Manager {
	hostname := strings.TrimSpace(options.MDNSHostname)
	if hostname == "" {
		hostname = "dropserve.local."
	}
	if !strings.HasSuffix(hostname, ".") {
		hostname += "."
	}
	port := options.HTTPPort
	if port == 0 {
		port = 80
	}
	logf := options.Logf
	if logf == nil {
		logf = log.Printf
	}
	register := options.RegisterMDNS
	if register == nil {
		register = registerZeroconf
	}
	manager := &Manager{
		snapshot:     Snapshot{LANIP: options.LANIP},
		httpPort:     port,
		mdnsHostname: hostname,
		logf:         logf,
		registerMDNS: register,
		noticePath:   options.NoticePath,
	}
	manager.initializeNetworkState()
	return manager
}

// StartMDNS starts best-effort local-name advertising. Every failure is
// logged and deliberately leaves the .local hostname out of Snapshot.
func (manager *Manager) StartMDNS() {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.mdnsRequested = true
	if manager.responder != nil || manager.snapshot.MDNSHostname != "" {
		manager.mu.Unlock()
		return
	}
	registration := MDNSRegistration{
		Hostname: manager.mdnsHostname,
		Port:     manager.httpPort,
	}
	if manager.snapshot.LANIP.IsValid() {
		registration.Addresses = []string{manager.snapshot.LANIP.String()}
	}
	manager.mu.Unlock()

	if len(registration.Addresses) == 0 {
		manager.logf("mDNS unavailable: no verified LAN address to advertise")
		return
	}
	responder, err := manager.registerMDNS(registration)
	if err != nil {
		manager.logf("mDNS unavailable: %v", err)
		return
	}

	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		responder.Shutdown()
		return
	}
	manager.responder = responder
	manager.snapshot.MDNSHostname = strings.TrimSuffix(registration.Hostname, ".")
	manager.mu.Unlock()
}

// Snapshot returns a copy of the current verified address surface.
func (manager *Manager) Snapshot() Snapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	snapshot := manager.snapshot
	if snapshot.LANChange != nil {
		change := *snapshot.LANChange
		snapshot.LANChange = &change
	}
	return snapshot
}

// UpdateLANIP publishes a newly verified LAN address and re-advertises mDNS
// when an active responder was bound to the old address.
func (manager *Manager) UpdateLANIP(address netip.Addr) {
	manager.mu.Lock()
	if manager.snapshot.LANIP == address {
		manager.mu.Unlock()
		return
	}
	oldAddress := manager.snapshot.LANIP
	manager.snapshot.LANIP = address
	if oldAddress.IsValid() && address.IsValid() && oldAddress != address {
		manager.snapshot.LANChange = &LANChange{OldLANIP: oldAddress.String(), NewLANIP: address.String()}
	}
	if address.IsValid() {
		manager.lastLANIP = address.String()
	}
	if err := manager.saveNetworkStateLocked(); err != nil {
		manager.logf("persist LAN address change: %v", err)
	}
	responder := manager.responder
	manager.responder = nil
	manager.snapshot.MDNSHostname = ""
	shouldStartMDNS := manager.mdnsRequested && address.IsValid()
	manager.mu.Unlock()
	if responder != nil {
		responder.Shutdown()
	}
	if shouldStartMDNS {
		manager.StartMDNS()
	}
}

// UpdateTailscale publishes the latest installed-client status.
func (manager *Manager) UpdateTailscale(status TailscaleStatus) {
	manager.mu.Lock()
	manager.snapshot.Tailscale = status
	manager.mu.Unlock()
}

// SetTailscaleServeEnabled publishes the verified state of the tailnet-only
// HTTPS proxy owned by the installed Tailscale client.
func (manager *Manager) SetTailscaleServeEnabled(enabled bool) error {
	manager.mu.Lock()
	manager.snapshot.Tailscale.ServeEnabled = enabled
	manager.mu.Unlock()
	return nil
}

// DismissNetworkChange removes the persisted DHCP/address notice.
func (manager *Manager) DismissNetworkChange() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	previous := manager.snapshot.LANChange
	manager.snapshot.LANChange = nil
	if err := manager.saveNetworkStateLocked(); err != nil {
		manager.snapshot.LANChange = previous
		return err
	}
	return nil
}

// Close stops any active mDNS responder.
func (manager *Manager) Close() {
	manager.mu.Lock()
	manager.closed = true
	responder := manager.responder
	manager.responder = nil
	manager.snapshot.MDNSHostname = ""
	manager.mu.Unlock()
	if responder != nil {
		responder.Shutdown()
	}
}

func registerZeroconf(registration MDNSRegistration) (MDNSResponder, error) {
	return zeroconf.RegisterProxy(
		"Dropserve",
		"_http._tcp",
		"local.",
		registration.Port,
		registration.Hostname,
		registration.Addresses,
		[]string{"path=/"},
		nil,
	)
}
