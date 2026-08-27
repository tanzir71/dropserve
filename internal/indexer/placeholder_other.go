//go:build !windows

package indexer

import "os"

func isCloudPlaceholder(_ string, _ os.FileInfo) (bool, error) {
	return false, nil
}
