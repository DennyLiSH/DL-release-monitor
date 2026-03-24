package models

import (
	"time"
)

// Repo represents a GitHub repository to monitor
type Repo struct {
	ID            int64     `json:"id" gorm:"primaryKey"`
	Owner         string    `json:"owner" gorm:"not null"`
	Name          string    `json:"name" gorm:"not null"`
	FullName      string    `json:"full_name" gorm:"uniqueIndex;not null"`
	Enabled       bool      `json:"enabled" gorm:"default:true"`
	CheckInterval int       `json:"check_interval" gorm:"default:0"` // 0 means use global default
	Retention     int       `json:"retention" gorm:"default:0"`      // 0 means use global default
	LastCheckedAt time.Time `json:"last_checked_at"`
	CreatedAt     time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// Relations
	Releases []Release `json:"releases,omitempty" gorm:"foreignKey:RepoID"`
}

// Release represents a GitHub release
type Release struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	RepoID      int64     `json:"repo_id" gorm:"index;not null"`
	GitHubID    int64     `json:"github_id" gorm:"uniqueIndex"`
	TagName     string    `json:"tag_name" gorm:"not null"`
	Version     string    `json:"version"`    // parsed version without 'v' prefix
	Major       int       `json:"major"`      // major version number
	Minor       int       `json:"minor"`      // minor version number
	Patch       int       `json:"patch"`      // patch version number
	Prerelease  bool      `json:"prerelease"` // is prerelease
	Draft       bool      `json:"draft"`      // is draft
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body" gorm:"type:text"` // release notes
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`

	// Relations
	Assets []Asset `json:"assets,omitempty" gorm:"foreignKey:ReleaseID"`
}

// Asset represents a release asset (downloadable file)
type Asset struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	ReleaseID    int64     `json:"release_id" gorm:"index;not null"`
	GitHubID     int64     `json:"github_id" gorm:"uniqueIndex"`
	Name         string    `json:"name" gorm:"not null"`
	Type         string    `json:"type"`          // installer, portable, source, checksum, other
	Size         int64     `json:"size"`          // file size in bytes
	DownloadURL  string    `json:"download_url"`  // GitHub download URL
	SHA256       string    `json:"sha256"`        // calculated after download
	LocalPath    string    `json:"local_path"`    // local storage path
	Status       string    `json:"status"`        // pending, downloading, done, failed
	ErrorMessage string    `json:"error_message"` // error if failed
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// DownloadLog represents a download history entry
type DownloadLog struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	AssetID   int64     `json:"asset_id" gorm:"index"`
	RepoName  string    `json:"repo_name"`
	Version   string    `json:"version"`
	FileName  string    `json:"file_name"`
	FileSize  int64     `json:"file_size"`
	Duration  int64     `json:"duration"` // download time in milliseconds
	Success   bool      `json:"success"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

// AssetType constants
const (
	AssetTypeInstaller = "installer" // .exe, .msi, .dmg, .pkg, .deb, .rpm, .apk
	AssetTypePortable  = "portable"  // .zip, .tar.gz, .tar.bz2, .tar.xz, .7z, .AppImage
	AssetTypeSource    = "source"    // source archives
	AssetTypeChecksum  = "checksum"  // .sha256, .md5, .asc
	AssetTypeOther     = "other"     // anything else
)

// AssetStatus constants
const (
	AssetStatusPending     = "pending"
	AssetStatusDownloading = "downloading"
	AssetStatusDone        = "done"
	AssetStatusFailed      = "failed"
)
