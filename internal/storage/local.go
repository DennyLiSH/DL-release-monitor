package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalStorage handles local file storage
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new local storage
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &LocalStorage{basePath: basePath}, nil
}

// Download downloads a file from URL to local storage
func (s *LocalStorage) Download(url, repoName, filename string) (localPath, sha256sum string, duration int64, err error) {
	start := time.Now()

	// Create repo directory
	repoDir := filepath.Join(s.basePath, sanitizePath(repoName))
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return "", "", 0, fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Destination path
	destPath := filepath.Join(repoDir, filename)

	// Create HTTP request with GitHub token in header for private repos
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	// Download file
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Create destination file
	out, err := os.Create(destPath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Calculate SHA256 while downloading
	hash := sha256.New()
	writer := io.MultiWriter(out, hash)

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		os.Remove(destPath)
		return "", "", 0, fmt.Errorf("failed to write file: %w", err)
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
		return 0, err
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
	return totalSize, err
}

// AvailableSpace returns available disk space (platform-dependent)
// For MVP, this returns 0. Can be enhanced with platform-specific implementations.
func (s *LocalStorage) AvailableSpace() (uint64, error) {
	// TODO: Implement platform-specific disk space check
	// For now, return 0 to indicate unknown
	return 0, nil
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
