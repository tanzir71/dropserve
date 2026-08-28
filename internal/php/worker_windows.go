//go:build windows

package php

import (
	"os/exec"
	"syscall"
)

func configureWorkerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
