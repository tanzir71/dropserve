package discovery

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// ProbeLANIP selects the primary private IPv4 from the host's current network
// surface. A UDP dial discovers the default route without sending traffic.
func ProbeLANIP() (netip.Addr, error) {
	defaultAddress := defaultRouteAddress()
	interfaces, err := net.Interfaces()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("list network interfaces: %w", err)
	}
	adapters := make([]Adapter, 0, len(interfaces))
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		adapter := Adapter{
			Name:     iface.Name,
			Up:       iface.Flags&net.FlagUp != 0,
			Loopback: iface.Flags&net.FlagLoopback != 0,
		}
		for _, raw := range addresses {
			prefix, err := netip.ParsePrefix(raw.String())
			if err != nil {
				continue
			}
			address := prefix.Addr().Unmap()
			adapter.Addresses = append(adapter.Addresses, address)
			if defaultAddress.IsValid() && address == defaultAddress {
				adapter.DefaultRoute = true
			}
		}
		adapters = append(adapters, adapter)
	}
	address, found := SelectLANIP(adapters)
	if !found {
		return netip.Addr{}, nil
	}
	return address, nil
}

func defaultRouteAddress() netip.Addr {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "udp4", "192.0.2.1:80")
	if err != nil {
		return netip.Addr{}
	}
	defer func() { _ = connection.Close() }()
	host, _, err := net.SplitHostPort(connection.LocalAddr().String())
	if err != nil {
		return netip.Addr{}
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap()
}
