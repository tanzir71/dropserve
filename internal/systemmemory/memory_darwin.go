//go:build darwin

package systemmemory

import "golang.org/x/sys/unix"

func totalBytes() (uint64, error) {
	return unix.SysctlUint64("hw.memsize")
}
