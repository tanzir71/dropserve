package autostart

import "fmt"

func verifyEnabled(name string, status func() (bool, error)) error {
	enabled, err := status()
	if err != nil {
		return fmt.Errorf("verify %s: %w", name, err)
	}
	if !enabled {
		return fmt.Errorf("verify %s: registration was created but is not enabled", name)
	}
	return nil
}
