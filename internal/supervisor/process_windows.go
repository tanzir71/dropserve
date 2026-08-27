//go:build windows

package supervisor

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processControl struct {
	mu  sync.Mutex
	job windows.Handle
}

func newProcessControl() (*processControl, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		// #nosec G103 -- the Windows API requires a pointer to this fixed layout.
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &processControl{job: job}, nil
}

func (control *processControl) configure(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func (control *processControl) attach(command *exec.Cmd) error {
	// #nosec G115 -- Windows process identifiers are unsigned 32-bit values.
	processID := uint32(command.Process.Pid)
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_INFORMATION,
		false,
		processID,
	)
	if err != nil {
		return err
	}
	defer func() {
		_ = windows.CloseHandle(process)
	}()
	return windows.AssignProcessToJobObject(control.job, process)
}

func (control *processControl) stop(_ *exec.Cmd) error {
	return control.close()
}

func (control *processControl) close() error {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.job == 0 {
		return nil
	}
	err := windows.CloseHandle(control.job)
	control.job = 0
	return err
}
