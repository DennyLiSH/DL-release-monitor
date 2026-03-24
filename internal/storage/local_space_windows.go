//go:build windows

package storage

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// availableSpace returns available disk space on Windows
func availableSpace(basePath string) (uint64, error) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return 0, fmt.Errorf("failed to get base path: %w", err)
	}

	// GetDiskFreeSpaceEx requires a directory path with trailing backslash for root
	pathPtr, err := windows.UTF16PtrFromString(absBase)
	if err != nil {
		return 0, fmt.Errorf("failed to convert path: %w", err)
	}

	var freeBytes uint64
	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeBytes, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk free space: %w", err)
	}

	return freeBytes, nil
}
