package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gh-release-monitor/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connection pool settings for SQLite
const (
	maxOpenConns    = 1                // SQLite doesn't support multiple writers
	maxIdleConns    = 1                // Single idle connection is sufficient
	connMaxLifetime = time.Hour        // Recreate connections periodically
)

// Init initializes the database connection and runs migrations
// The database file will be created in the same directory as storagePath
func Init(storagePath string) (*gorm.DB, error) {
	// Database path: same parent directory as storage, with "releases.db" filename
	// e.g., storagePath = "./data/downloads" -> dbPath = "./data/releases.db"
	dbPath := filepath.Join(filepath.Dir(storagePath), "releases.db")

	// Ensure directory exists (sqlite driver creates the file, but not the directory)
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection with connection pool settings
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000", dbPath)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

	// Verify connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run auto migration
	if err := db.AutoMigrate(
		&models.Repo{},
		&models.Release{},
		&models.Asset{},
		&models.DownloadLog{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return db, nil
}
