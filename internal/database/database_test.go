package database

import (
	"os"
	"path/filepath"
	"testing"

	"gh-release-monitor/internal/models"

	"gorm.io/gorm"
)

func TestInit(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "downloads")

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "successful initialization",
			path:    storagePath,
			wantErr: false,
		},
		{
			name:    "nested directory creation",
			path:    filepath.Join(tempDir, "nested", "deep", "downloads"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := Init(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				// Verify database is usable
				var count int64
				if err := db.Model(&models.Repo{}).Count(&count).Error; err != nil {
					t.Errorf("Failed to query database: %v", err)
				}

				// Verify migrations ran
				if !db.Migrator().HasTable(&models.Repo{}) {
					t.Error("Repo table was not created")
				}
				if !db.Migrator().HasTable(&models.Release{}) {
					t.Error("Release table was not created")
				}
				if !db.Migrator().HasTable(&models.Asset{}) {
					t.Error("Asset table was not created")
				}
				if !db.Migrator().HasTable(&models.DownloadLog{}) {
					t.Error("DownloadLog table was not created")
				}

				// Close database
				sqlDB, err := db.DB()
				if err != nil {
					t.Errorf("Failed to get underlying DB: %v", err)
				}
				sqlDB.Close()
			}
		})
	}
}

func TestInit_DatabaseFileCreation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "downloads")
	db, err := Init(storagePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Close database
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("Failed to get underlying DB: %v", err)
	}
	sqlDB.Close()

	// Verify database file was created in parent directory
	dbPath := filepath.Join(filepath.Dir(storagePath), "releases.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file was not created at %s", dbPath)
	}
}

func TestInit_ConnectionPool(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "downloads")
	db, err := Init(storagePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("Failed to get underlying DB: %v", err)
	}

	// Verify connection pool settings
	stats := sqlDB.Stats()
	if stats.MaxOpenConnections != maxOpenConns {
		t.Errorf("MaxOpenConnections = %d, want %d", stats.MaxOpenConnections, maxOpenConns)
	}
}

func TestInit_CRUD(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "downloads")
	db, err := Init(storagePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	// Test Create
	repo := models.Repo{
		Owner:    "testowner",
		Name:     "testrepo",
		FullName: "testowner/testrepo",
		Enabled:  true,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("Failed to create repo: %v", err)
	}

	// Test Read
	var foundRepo models.Repo
	if err := db.First(&foundRepo, repo.ID).Error; err != nil {
		t.Fatalf("Failed to read repo: %v", err)
	}
	if foundRepo.FullName != repo.FullName {
		t.Errorf("FullName = %s, want %s", foundRepo.FullName, repo.FullName)
	}

	// Test Update
	if err := db.Model(&foundRepo).Update("enabled", false).Error; err != nil {
		t.Fatalf("Failed to update repo: %v", err)
	}
	var updatedRepo models.Repo
	if err := db.First(&updatedRepo, repo.ID).Error; err != nil {
		t.Fatalf("Failed to read updated repo: %v", err)
	}
	if updatedRepo.Enabled {
		t.Error("Enabled should be false after update")
	}

	// Test Delete
	if err := db.Delete(&foundRepo).Error; err != nil {
		t.Fatalf("Failed to delete repo: %v", err)
	}
	var deletedRepo models.Repo
	err = db.First(&deletedRepo, repo.ID).Error
	if err == nil {
		t.Error("Repo should be deleted")
	}
	if err != gorm.ErrRecordNotFound {
		t.Errorf("Expected ErrRecordNotFound, got: %v", err)
	}
}
