//go:build windows

package server_test

import (
	"errors"
	"strconv"

	"golang.org/x/sys/windows"
)

func processAlive(processID uint32) (bool, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, processID)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, err
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()
	event, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	switch event {
	case windows.WAIT_OBJECT_0:
		return false, nil
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	default:
		return false, errors.New("unexpected process wait result: " + strconv.FormatUint(uint64(event), 10))
	}
}
