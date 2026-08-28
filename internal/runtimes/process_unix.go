//go:build !windows

package runtimes

import "os/exec"

func configureRuntimeCommand(_ *exec.Cmd) {}
