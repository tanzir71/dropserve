package runtimes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DatabaseDataDirectory creates one engine's mutable data boundary below the
// Dropserve state directory. App paths are not accepted by this API.
func DatabaseDataDirectory(stateDirectory, engine string) (string, error) {
	if strings.TrimSpace(stateDirectory) == "" {
		return "", fmt.Errorf("database state directory is required")
	}
	switch engine {
	case "mariadb", "postgres":
	default:
		return "", fmt.Errorf("unsupported database engine %q", engine)
	}
	root, err := filepath.Abs(stateDirectory)
	if err != nil {
		return "", fmt.Errorf("resolve database state directory: %w", err)
	}
	if root == filepath.VolumeName(root)+string(filepath.Separator) {
		return "", fmt.Errorf("refuse to use filesystem root as database state directory")
	}
	directory := filepath.Join(root, "databases", engine, "data")
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("database data directory escapes Dropserve state")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create %s data directory: %w", engine, err)
	}
	return directory, nil
}
