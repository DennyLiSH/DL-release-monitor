package database

import (
	"fmt"
	"path/filepath"

	"gh-release-monitor/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the global database instance
var DB *gorm.DB

// Init initializes the database connection and runs migrations
func Init(storagePath string) (*gorm.DB, error) {
	// Determine database path
	dbPath := filepath.Join(filepath.Dir(storagePath), "data", "releases.db")

	// Ensure directory exists
	// Note: sqlite driver will create the file, but not the directory

	// Open database connection
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_foreign_keys=ON", dbPath)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
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

	DB = db
	return db, nil
}

// GetDB returns the database instance
func GetDB() *gorm.DB {
	return DB
}
