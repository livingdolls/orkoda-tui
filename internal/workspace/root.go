package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrepareRoot creates and resolves the daemon-managed workspace root while
// rejecting a root path that is itself a symbolic link.
func PrepareRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if info, err := os.Lstat(absolute); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("workspace root must not be a symbolic link")
		}
		if !info.IsDir() {
			return "", fmt.Errorf("workspace root must be a directory")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect workspace root: %w", err)
	} else if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create workspace root: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	return filepath.Clean(resolved), nil
}
