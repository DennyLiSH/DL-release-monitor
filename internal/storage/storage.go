package storage

import (
	"context"
)

// Storage defines the interface for file storage operations.
// Implementations can support different backends (local filesystem, S3, GCS, etc.)
type Storage interface {
	// Download downloads a file from URL to storage.
	// Returns the local path, SHA256 checksum, duration in milliseconds, and any error.
	Download(ctx context.Context, url, repoName, filename string) (localPath, sha256sum string, duration int64, err error)

	// Delete removes a file from storage.
	Delete(localPath string) error

	// Exists checks if a file exists in storage.
	Exists(localPath string) bool

	// Size returns the size of a file in bytes.
	Size(localPath string) (int64, error)

	// DiskUsage returns the total disk usage of the storage in bytes.
	DiskUsage() (int64, error)

	// AvailableSpace returns the available disk space in bytes.
	AvailableSpace() (uint64, error)
}
