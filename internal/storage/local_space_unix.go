//go:build !windows

package storage

import (
	"fmt"
	"path/filepath"
	"syscall"
)

// availableSpace returns available disk space on Unix-like systems
func availableSpace(basePath string) (uint64, error) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return 0, fmt.Errorf("failed to get base path: %w", err)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(absBase, &stat); err != nil {
		return 0, fmt.Errorf("failed to get disk stats: %w", err)
	}

	// Bavail is available blocks for non-root users
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
