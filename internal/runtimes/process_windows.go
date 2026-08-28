//go:build windows

package runtimes

import (
	"os/exec"
	"syscall"
)

func configureRuntimeCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
}
