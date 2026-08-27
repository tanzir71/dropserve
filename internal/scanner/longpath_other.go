//go:build !windows

package scanner

func pathForWalk(path string) (string, error) {
	return path, nil
}
