package main

import (
	"os"
	"strconv"
)

const backgroundConsoleProbeEnvironment = "DROPSERVE_BACKGROUND_CONSOLE_PROBE"

func writeBackgroundConsoleProbe() error {
	probePath := os.Getenv(backgroundConsoleProbeEnvironment)
	if probePath == "" {
		return nil
	}
	value := strconv.FormatUint(uint64(backgroundConsoleWindow()), 10)
	// #nosec G304,G703 -- this opt-in diagnostic path is supplied by the local acceptance smoke.
	return os.WriteFile(probePath, []byte(value), 0o600)
}
