package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewLocalStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}
	if storage == nil {
		t.Fatal("storage is nil")
	}
	if storage.basePath != tmpDir {
		t.Errorf("basePath = %q, want %q", storage.basePath, tmpDir)
	}
}

func TestNewLocalStorage_CreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	newDir := filepath.Join(tmpDir, "subdir", "nested")
	_, err = NewLocalStorage(newDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Errorf("directory %q was not created", newDir)
	}
}

func TestDownload_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, _, err = storage.Download(ctx, "http://example.com/file.zip", "test/repo", "file.zip")
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Test non-existent file
	if storage.Exists("/nonexistent/file.txt") {
		t.Error("Exists should return false for non-existent file")
	}

	// Test empty path
	if storage.Exists("") {
		t.Error("Exists should return false for empty path")
	}

	// Test existing file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if !storage.Exists(testFile) {
		t.Error("Exists should return true for existing file")
	}
}

func TestSize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Test non-existent file
	_, err = storage.Size("/nonexistent/file.txt")
	if err == nil {
		t.Error("Size should return error for non-existent file")
	}

	// Test existing file
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("hello world")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatal(err)
	}

	size, err := storage.Size(testFile)
	if err != nil {
		t.Fatalf("Size failed: %v", err)
	}
	if size != int64(len(testContent)) {
		t.Errorf("Size = %d, want %d", size, len(testContent))
	}
}

func TestDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Test deleting empty path
	if err := storage.Delete(""); err != nil {
		t.Error("Delete should return nil for empty path")
	}

	// Test deleting file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := storage.Delete(testFile); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestDelete_PathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Try to delete file outside storage directory
	outsideFile := filepath.Join(filepath.Dir(tmpDir), "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outsideFile)

	err = storage.Delete(outsideFile)
	if err == nil {
		t.Error("Delete should reject path outside storage directory")
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"owner/repo", "owner_repo"},
		{"owner\\repo", "owner_repo"},
		{"..secret", "_secret"},
		{"normal", "normal"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizePath(tt.input)
			if got != tt.want {
				t.Errorf("sanitizePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file.txt", "file.txt"},
		{"../secret.txt", "secret.txt"},
		{"path/to/file.txt", "file.txt"},
		{"", ""},
		{".", ""},
		{"..", ""},
		{"file\u0000name.txt", "filename.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
