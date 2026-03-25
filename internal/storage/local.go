package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Default download timeout
const DefaultDownloadTimeout = 10 * time.Minute

// LocalStorage implements the Storage interface for local filesystem operations.
// It provides thread-safe file download, deletion, and management capabilities.
type LocalStorage struct {
	basePath       string
	downloadClient *http.Client
}

// NewLocalStorage creates a new local storage
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &LocalStorage{
		basePath: basePath,
		downloadClient: &http.Client{
			Timeout: DefaultDownloadTimeout,
		},
	}, nil
}

// Download downloads a file from URL to local storage with context support
func (s *LocalStorage) Download(ctx context.Context, url, repoName, filename string) (localPath, sha256sum string, duration int64, err error) {
	start := time.Now()

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", "", 0, fmt.Errorf("download canceled: %w", err)
	}

	// Create repo directory
	repoDir := filepath.Join(s.basePath, sanitizePath(repoName))
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return "", "", 0, fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Sanitize filename to prevent path traversal
	safeFilename := sanitizeFilename(filename)
	if safeFilename == "" {
		return "", "", 0, fmt.Errorf("invalid filename: %s", filename)
	}

	// Destination path
	destPath := filepath.Join(repoDir, safeFilename)

	// Create HTTP request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	// Download file using configured client
	resp, err := s.downloadClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", "", 0, fmt.Errorf("download canceled: %w", err)
		}
		return "", "", 0, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Write to temp file first, then atomic rename to avoid partial writes
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create temp file: %w", err)
	}

	// Calculate SHA256 while downloading
	hash := sha256.New()
	writer := io.MultiWriter(out, hash)

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		out.Close()
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("warning: failed to remove temp file %s: %v", tmpPath, removeErr)
		}
		if errors.Is(err, context.Canceled) {
			return "", "", 0, fmt.Errorf("download canceled during write: %w", err)
		}
		return "", "", 0, fmt.Errorf("failed to write file: %w", err)
	}

	// Close file before renaming
	if err := out.Close(); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("warning: failed to remove temp file %s: %v", tmpPath, removeErr)
		}
		return "", "", 0, fmt.Errorf("failed to close file: %w", err)
	}

	// Atomic rename from temp to final destination
	if err := os.Rename(tmpPath, destPath); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("warning: failed to remove temp file %s: %v", tmpPath, removeErr)
		}
		return "", "", 0, fmt.Errorf("failed to rename temp file: %w", err)
	}

	sha256sum = hex.EncodeToString(hash.Sum(nil))
	duration = time.Since(start).Milliseconds()

	return destPath, sha256sum, duration, nil
}

// Delete removes a file from local storage
func (s *LocalStorage) Delete(localPath string) error {
	if localPath == "" {
		return nil
	}

	// Security check: ensure path is within base directory
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	absBase, err := filepath.Abs(s.basePath)
	if err != nil {
		return fmt.Errorf("failed to get base path: %w", err)
	}

	if !strings.HasPrefix(absPath, absBase) {
		return fmt.Errorf("path outside storage directory")
	}

	return os.Remove(localPath)
}

// Exists checks if a file exists
func (s *LocalStorage) Exists(localPath string) bool {
	if localPath == "" {
		return false
	}
	_, err := os.Stat(localPath)
	return err == nil
}

// Size returns the size of a file
func (s *LocalStorage) Size(localPath string) (int64, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get file size: %w", err)
	}
	return info.Size(), nil
}

// DiskUsage returns the total disk usage of the storage
func (s *LocalStorage) DiskUsage() (int64, error) {
	var totalSize int64
	err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to calculate disk usage: %w", err)
	}
	return totalSize, nil
}

// AvailableSpace returns available disk space
// Platform-specific implementations are in local_space_*.go files
func (s *LocalStorage) AvailableSpace() (uint64, error) {
	return availableSpace(s.basePath)
}

// sanitizePath removes potentially dangerous characters from path components
func sanitizePath(path string) string {
	// Replace path separators and other dangerous characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"..", "_",
	)
	return replacer.Replace(path)
}

// sanitizeFilename cleans a filename to prevent path traversal attacks
func sanitizeFilename(filename string) string {
	// Use filepath.Base to strip any directory components
	filename = filepath.Base(filename)

	// Remove null bytes and other control characters
	var sb strings.Builder
	for _, r := range filename {
		if r == 0 || (r < 32 && r != '\t') {
			continue
		}
		sb.WriteRune(r)
	}

	result := sb.String()

	// Don't allow empty filenames or just dots
	if result == "" || result == "." || result == ".." {
		return ""
	}

	return result
}
