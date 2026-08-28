//go:build !windows && !linux && !darwin

package systemmemory

import "errors"

func totalBytes() (uint64, error) {
	return 0, errors.New("physical memory probe is unavailable")
}
