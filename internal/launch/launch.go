// Package launch opens local product pages in the user's default browser.
package launch

import (
	"context"
	"fmt"
	"os/exec"
)

func startAndRelease(name string, arguments ...string) error {
	// #nosec G204 -- callers provide a fixed OS browser launcher and a loopback HTTP URL.
	command := exec.CommandContext(context.Background(), name, arguments...)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start default browser: %w", err)
	}
	go func() {
		_ = command.Wait()
	}()
	return nil
}
