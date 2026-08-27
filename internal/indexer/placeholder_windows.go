//go:build windows

package indexer

import (
	"os"
	"syscall"
)

const fileAttributeRecallOnDataAccess = 0x00400000

func isCloudPlaceholder(_ string, info os.FileInfo) (bool, error) {
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return false, nil
	}
	return attributes.FileAttributes&fileAttributeRecallOnDataAccess != 0, nil
}
