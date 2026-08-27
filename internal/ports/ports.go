// Package ports acquires HTTP listeners by probing the configured fallback ladder.
package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
)

var fallbackLadder = []int{80, 8080, 8000, 8888, 3000, 0}

// Selection describes the listener chosen by the probe.
type Selection struct {
	Port      int
	Attempted []int
	Fallback  bool
	Message   string
}

// Acquire probes real TCP listeners, preferring a previously persisted port
// when supplied and otherwise following the product fallback ladder.
func Acquire(ctx context.Context, bind string, preferred int) (net.Listener, Selection, error) {
	candidates := make([]int, 0, len(fallbackLadder)+1)
	if preferred > 0 {
		candidates = append(candidates, preferred)
	}
	for _, candidate := range fallbackLadder {
		if candidate != preferred {
			candidates = append(candidates, candidate)
		}
	}

	var attempts []int
	var failures []error
	var listenConfig net.ListenConfig
	for _, candidate := range candidates {
		attempts = append(attempts, candidate)
		address := net.JoinHostPort(bind, strconv.Itoa(candidate))
		listener, err := listenConfig.Listen(ctx, "tcp", address)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", address, err))
			continue
		}
		_, portText, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			_ = listener.Close()
			failures = append(failures, fmt.Errorf("read selected listener %q: %w", listener.Addr(), err))
			continue
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			_ = listener.Close()
			failures = append(failures, fmt.Errorf("read selected port %q: %w", portText, err))
			continue
		}
		selection := Selection{Port: port, Attempted: attempts, Fallback: port != 80}
		if selection.Fallback {
			selection.Message = fmt.Sprintf(
				"Port 80 is being used by another program, so Dropserve is using port %d instead.",
				port,
			)
		}
		return listener, selection, nil
	}
	return nil, Selection{Attempted: attempts}, fmt.Errorf("no HTTP port was available: %w", errors.Join(failures...))
}
