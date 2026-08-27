//go:build !windows

package supervisor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type processControl struct{}

func newProcessControl() (*processControl, error) {
	return &processControl{}, nil
}

func (*processControl) configure(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (*processControl) attach(_ *exec.Cmd) error {
	return nil
}

func (*processControl) stop(command *exec.Cmd) error {
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (*processControl) close() error {
	return nil
}
