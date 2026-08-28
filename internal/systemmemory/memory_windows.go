//go:build windows

package systemmemory

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	Length            uint32
	MemoryLoad        uint32
	TotalPhysical     uint64
	AvailablePhysical uint64
	TotalPageFile     uint64
	AvailablePageFile uint64
	TotalVirtual      uint64
	AvailableVirtual  uint64
	AvailableExtended uint64
}

func totalBytes() (uint64, error) {
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))
	procedure := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	// #nosec G103 -- GlobalMemoryStatusEx requires a pointer to this fixed Windows ABI structure.
	result, _, callErr := procedure.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return 0, fmt.Errorf("GlobalMemoryStatusEx: %w", callErr)
	}
	return status.TotalPhysical, nil
}
