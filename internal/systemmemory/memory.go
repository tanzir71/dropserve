// Package systemmemory reads total physical memory for the automatic lazy-start policy.
package systemmemory

// LowMemory reports whether total physical memory is below eight GiB. An
// unavailable platform probe conservatively keeps eager startup enabled.
func LowMemory() bool {
	total, err := totalBytes()
	return err == nil && total > 0 && total < 8<<30
}
