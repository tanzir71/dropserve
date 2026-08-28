package discovery

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"time"
)

// MonitorOptions supplies network probing and listener recovery boundaries.
type MonitorOptions struct {
	Interval        time.Duration
	Events          <-chan struct{}
	Manager         *Manager
	ProbeLANIP      func() (netip.Addr, error)
	ProbeTailscale  func(context.Context) (TailscaleStatus, error)
	ListenerHealthy func() bool
	RecoverListener func(context.Context) error
	ExpireFunnels   func(context.Context) error
	Logf            func(string, ...any)
}

// Monitor revalidates network and listener state after events and on a bounded
// periodic interval.
type Monitor struct {
	options MonitorOptions
}

// NewMonitor creates a network recovery monitor.
func NewMonitor(options MonitorOptions) *Monitor {
	if options.Interval <= 0 {
		options.Interval = 30 * time.Second
	}
	if options.Logf == nil {
		options.Logf = log.Printf
	}
	return &Monitor{options: options}
}

// Run checks until the parent context ends.
func (monitor *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(monitor.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-monitor.options.Events:
		case <-ticker.C:
		}
		if err := monitor.Check(ctx); err != nil {
			monitor.options.Logf("network recovery check failed: %v", err)
		}
	}
}

// Check performs one re-probe and listener-health pass.
func (monitor *Monitor) Check(ctx context.Context) error {
	if monitor.options.ProbeLANIP == nil {
		return fmt.Errorf("LAN probe is not configured")
	}
	address, err := monitor.options.ProbeLANIP()
	if err != nil {
		return fmt.Errorf("probe LAN address: %w", err)
	}
	if monitor.options.Manager != nil {
		monitor.options.Manager.UpdateLANIP(address)
		if monitor.options.ProbeTailscale != nil {
			status, probeErr := monitor.options.ProbeTailscale(ctx)
			if probeErr != nil {
				monitor.options.Logf("Tailscale status refresh failed: %v", probeErr)
			} else {
				monitor.options.Manager.UpdateTailscale(status)
			}
		}
	}
	if monitor.options.ListenerHealthy != nil && !monitor.options.ListenerHealthy() {
		if monitor.options.RecoverListener == nil {
			return fmt.Errorf("HTTP listener is down and recovery is not configured")
		}
		if err := monitor.options.RecoverListener(ctx); err != nil {
			return fmt.Errorf("recover HTTP listener: %w", err)
		}
	}
	if monitor.options.ExpireFunnels != nil {
		if err := monitor.options.ExpireFunnels(ctx); err != nil {
			return fmt.Errorf("expire public sharing: %w", err)
		}
	}
	return nil
}
