//go:build ignore

// Command mdns-spike performs the bounded M7 multicast-DNS compatibility check.
package main

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

const spikeDuration = 20 * time.Second

func main() {
	interfaces, addresses, err := multicastSurface()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mDNS spike failed before bind: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("platform: %s/%s; go: %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	for _, iface := range interfaces {
		fmt.Printf("multicast interface: %s (index %d)\n", iface.Name, iface.Index)
	}
	fmt.Printf("advertising: dropserve-spike.local. -> %v for %s\n", addresses, spikeDuration)
	server, err := zeroconf.RegisterProxy(
		"Dropserve Spike",
		"_http._tcp",
		"local.",
		8080,
		"dropserve-spike.local.",
		addresses,
		[]string{"path=/", "spike=m7"},
		interfaces,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mDNS bind failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("mDNS bind succeeded")
	time.Sleep(spikeDuration)
	server.Shutdown()
	fmt.Println("mDNS spike completed and shut down cleanly")
}

func multicastSurface() ([]net.Interface, []string, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, nil, fmt.Errorf("list interfaces: %w", err)
	}
	var interfaces []net.Interface
	var addresses []string
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		interfaceAddresses, err := iface.Addrs()
		if err != nil {
			return nil, nil, fmt.Errorf("list addresses for %s: %w", iface.Name, err)
		}
		added := false
		for _, address := range interfaceAddresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			addresses = append(addresses, ip.String())
			added = true
		}
		if added {
			interfaces = append(interfaces, iface)
		}
	}
	if len(interfaces) == 0 || len(addresses) == 0 {
		return nil, nil, fmt.Errorf("no active non-loopback multicast interface has an address")
	}
	return interfaces, addresses, nil
}
