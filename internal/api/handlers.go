package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gh-release-monitor/internal/config"
	"gh-release-monitor/internal/models"
	"gh-release-monitor/internal/storage"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

const (
	defaultReleaseLimit  = 10
	maxReleaseLimit      = 100
	defaultRepoLimit     = 50
	maxRepoLimit         = 100
	defaultDeleteTimeout = 30 * time.Second
)

// ListRepos returns all repositories with pagination
func (r *Router) ListRepos(w http.ResponseWriter, req *http.Request) {
	// Parse pagination parameters
	page, limit := parsePagination(req, defaultRepoLimit, maxRepoLimit)
	offset := (page - 1) * limit

	var repos []models.Repo
	if err := r.db.Order("created_at DESC").Offset(offset).Limit(limit).Find(&repos).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get total count for pagination
	var total int64
	if err := r.db.Model(&models.Repo{}).Count(&total).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	r.writeJSON(w, http.StatusOK, map[string]any{
		"data":  repos,
		"page":  page,
		"limit": limit,
		"total": total,
		"pages": (total + int64(limit) - 1) / int64(limit),
	})
}

// parsePagination extracts page and limit from query parameters
func parsePagination(req *http.Request, defaultLimit, maxLimit int) (page, limit int) {
	page = 1
	limit = defaultLimit

	if p := req.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	if l := req.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			if v > maxLimit {
				limit = maxLimit
			} else {
				limit = v
			}
		}
	}

	return page, limit
}

// CreateRepo creates a new repository
func (r *Router) CreateRepo(w http.ResponseWriter, req *http.Request) {
	var input struct {
		FullName      string `json:"full_name"`
		Enabled       bool   `json:"enabled"`
		CheckInterval int    `json:"check_interval"`
		Retention     int    `json:"retention"`
	}

	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// Parse owner/repo
	owner, name, err := config.ParseRepoFullName(input.FullName)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate repo exists on GitHub
	ctx := req.Context()
	if err := r.ghClient.ValidateRepo(ctx, owner, name); err != nil {
		r.writeError(w, http.StatusBadRequest, "Repository not accessible: "+err.Error())
		return
	}

	// Create repo
	repo := models.Repo{
		Owner:         owner,
		Name:          name,
		FullName:      input.FullName,
		Enabled:       input.Enabled,
		CheckInterval: input.CheckInterval,
		Retention:     input.Retention,
	}

	if err := r.db.Create(&repo).Error; err != nil {
		if isUniqueConstraintError(err) {
			r.writeError(w, http.StatusConflict, "Repository already exists")
			return
		}
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	r.writeJSON(w, http.StatusCreated, repo)
}

// GetRepo returns a single repository
func (r *Router) GetRepo(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	var repo models.Repo
	if err := r.db.First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			r.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Load releases
	if err := r.db.Where("repo_id = ?", repo.ID).Order("published_at DESC").Limit(defaultReleaseLimit).Find(&repo.Releases).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load releases: %v", err))
		return
	}

	r.writeJSON(w, http.StatusOK, repo)
}

// UpdateRepo updates a repository
func (r *Router) UpdateRepo(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	var repo models.Repo
	if err := r.db.First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			r.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var input struct {
		Enabled       *bool `json:"enabled"`
		CheckInterval *int  `json:"check_interval"`
		Retention     *int  `json:"retention"`
	}

	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	updates := map[string]any{}
	if input.Enabled != nil {
		updates["enabled"] = *input.Enabled
	}
	if input.CheckInterval != nil {
		updates["check_interval"] = *input.CheckInterval
	}
	if input.Retention != nil {
		updates["retention"] = *input.Retention
	}

	if len(updates) > 0 {
		if err := r.db.Model(&repo).Updates(updates).Error; err != nil {
			r.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	r.writeJSON(w, http.StatusOK, repo)
}

// DeleteRepo deletes a repository
func (r *Router) DeleteRepo(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	var repo models.Repo
	if err := r.db.First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			r.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 1. First, collect file paths to delete (before transaction)
	var assetPaths []string
	if err := r.db.Model(&models.Asset{}).
		Joins("JOIN releases ON releases.id = assets.release_id").
		Where("releases.repo_id = ?", repo.ID).
		Where("assets.local_path != ''").
		Pluck("assets.local_path", &assetPaths).Error; err != nil {
		log.Printf("Failed to fetch asset paths for deletion: %v", err)
	}

	// 2. Delete from database with transaction (atomic operation)
	err = r.db.Transaction(func(tx *gorm.DB) error {
		// Delete assets first
		if err := tx.Exec(`DELETE FROM assets WHERE release_id IN (SELECT id FROM releases WHERE repo_id = ?)`, repo.ID).Error; err != nil {
			return fmt.Errorf("failed to delete assets: %w", err)
		}
		// Delete releases
		if err := tx.Where("repo_id = ?", repo.ID).Delete(&models.Release{}).Error; err != nil {
			return fmt.Errorf("failed to delete releases: %w", err)
		}
		// Delete repo
		if err := tx.Delete(&repo).Error; err != nil {
			return fmt.Errorf("failed to delete repository: %w", err)
		}
		return nil
	})

	if err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 3. Database deleted successfully, now delete files asynchronously
	// This ensures data consistency even if file deletion fails
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic recovered in DeleteRepo cleanup: %v", r)
			}
		}()

		// Add timeout context to prevent goroutine from hanging indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), defaultDeleteTimeout)
		defer cancel()

		storageBackend, err := storage.NewLocalStorage(r.cfg.Storage.Local.Path)
		if err != nil {
			log.Printf("Failed to initialize storage for cleanup: %v", err)
			return
		}
		for _, path := range assetPaths {
			select {
			case <-ctx.Done():
				log.Printf("File cleanup timed out, some files may remain")
				return
			default:
				if err := storageBackend.Delete(path); err != nil {
					log.Printf("Warning: failed to delete file %s: %v", path, err)
				}
			}
		}
	}()

	r.writeJSON(w, http.StatusOK, map[string]string{"message": "Deleted"})
}

// ListReleases returns all releases
func (r *Router) ListReleases(w http.ResponseWriter, req *http.Request) {
	repoID := req.URL.Query().Get("repo_id")

	query := r.db.Model(&models.Release{}).Preload("Assets").Order("published_at DESC")
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}

	var releases []models.Release
	if err := query.Limit(maxReleaseLimit).Find(&releases).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	r.writeJSON(w, http.StatusOK, releases)
}

// ListDownloads returns download history
func (r *Router) ListDownloads(w http.ResponseWriter, req *http.Request) {
	var logs []models.DownloadLog
	if err := r.db.Order("created_at DESC").Limit(maxReleaseLimit).Find(&logs).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	r.writeJSON(w, http.StatusOK, logs)
}

// TriggerCheck triggers a check of all repos
func (r *Router) TriggerCheck(w http.ResponseWriter, req *http.Request) {
	r.sched.CheckNow()
	r.writeJSON(w, http.StatusOK, map[string]string{"message": "Check triggered"})
}

// TriggerRepoCheck triggers a check of a specific repo
func (r *Router) TriggerRepoCheck(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	if err := r.sched.CheckRepoNow(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			r.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		// Return the actual error for other cases (e.g., initialization failure)
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	r.writeJSON(w, http.StatusOK, map[string]string{"message": "Check triggered"})
}

// GetConfig returns current configuration
func (r *Router) GetConfig(w http.ResponseWriter, req *http.Request) {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()

	// Return sanitized config (no secrets)
	cfg := map[string]any{
		"server": map[string]any{
			"port":         r.cfg.Server.Port,
			"base_url":     r.cfg.Server.BaseURL,
			"auth_enabled": r.cfg.Server.AuthKey != "",
		},
		"github": map[string]any{
			"poll_interval": r.cfg.GitHub.PollInterval,
		},
		"storage": map[string]any{
			"local": map[string]any{
				"enabled": r.cfg.Storage.Local.Enabled,
				"path":    r.cfg.Storage.Local.Path,
			},
		},
		"retention": map[string]any{
			"max_versions":    r.cfg.Retention.MaxVersions,
			"keep_last_major": r.cfg.Retention.KeepLastMajor,
		},
		"notify": map[string]any{
			"email": map[string]any{
				"enabled": r.cfg.Notify.Email.Enabled,
			},
			"webhook": map[string]any{
				"enabled": r.cfg.Notify.Webhook.Enabled,
			},
		},
	}
	r.writeJSON(w, http.StatusOK, cfg)
}

// UpdateConfig updates configuration (limited fields)
func (r *Router) UpdateConfig(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Retention *struct {
			MaxVersions   *int  `json:"max_versions"`
			KeepLastMajor *bool `json:"keep_last_major"`
		} `json:"retention"`
	}

	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		r.writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	r.cfgMu.Lock()
	defer r.cfgMu.Unlock()

	if input.Retention != nil {
		if input.Retention.MaxVersions != nil {
			r.cfg.Retention.MaxVersions = *input.Retention.MaxVersions
		}
		if input.Retention.KeepLastMajor != nil {
			r.cfg.Retention.KeepLastMajor = *input.Retention.KeepLastMajor
		}
	}

	r.writeJSON(w, http.StatusOK, map[string]string{"message": "Config updated"})
}

// GetStatus returns system status
func (r *Router) GetStatus(w http.ResponseWriter, req *http.Request) {
	var errors []string
	var repoCount int64
	var releaseCount int64
	var assetCount int64
	var downloadCount int64

	if err := r.db.Model(&models.Repo{}).Count(&repoCount).Error; err != nil {
		log.Printf("Failed to count repos: %v", err)
		errors = append(errors, fmt.Sprintf("repo_count: %v", err))
	}
	if err := r.db.Model(&models.Release{}).Count(&releaseCount).Error; err != nil {
		log.Printf("Failed to count releases: %v", err)
		errors = append(errors, fmt.Sprintf("release_count: %v", err))
	}
	if err := r.db.Model(&models.Asset{}).Where("status = ?", models.AssetStatusDone).Count(&assetCount).Error; err != nil {
		log.Printf("Failed to count assets: %v", err)
		errors = append(errors, fmt.Sprintf("asset_count: %v", err))
	}
	if err := r.db.Model(&models.DownloadLog{}).Where("success = ?", true).Count(&downloadCount).Error; err != nil {
		log.Printf("Failed to count downloads: %v", err)
		errors = append(errors, fmt.Sprintf("download_count: %v", err))
	}

	// Calculate storage usage
	var storageSize int64
	if err := r.db.Model(&models.Asset{}).
		Where("status = ?", models.AssetStatusDone).
		Select("COALESCE(SUM(size), 0)").Row().Scan(&storageSize); err != nil {
		log.Printf("Failed to calculate storage size: %v", err)
		errors = append(errors, fmt.Sprintf("storage_size: %v", err))
	}

	statusVal := "running"
	if len(errors) > 0 {
		statusVal = "degraded"
	}

	status := map[string]any{
		"status":         statusVal,
		"uptime":         time.Since(r.startTime).String(),
		"repo_count":     repoCount,
		"release_count":  releaseCount,
		"asset_count":    assetCount,
		"download_count": downloadCount,
		"storage_size":   storageSize,
	}
	if len(errors) > 0 {
		status["errors"] = errors
	}

	r.writeJSON(w, http.StatusOK, status)
}

// HealthCheck returns basic health status
func (r *Router) HealthCheck(w http.ResponseWriter, req *http.Request) {
	r.writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// ReadyCheck returns readiness status (checks database connection)
func (r *Router) ReadyCheck(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	// Check database connectivity
	sqlDB, err := r.db.DB()
	if err != nil {
		r.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"error":  "failed to get database connection",
		})
		return
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		r.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"error":  "database ping failed",
		})
		return
	}

	r.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ready",
	})
}

// writeJSON writes JSON response
func (r *Router) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

// writeError writes error response
func (r *Router) writeError(w http.ResponseWriter, status int, message string) {
	r.writeJSON(w, status, map[string]string{"error": message})
}

// isUniqueConstraintError checks if the error is a unique constraint violation
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	// Check GORM's duplicated key error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	// Fallback to string matching for SQLite and other drivers
	errMsg := err.Error()
	return strings.Contains(errMsg, "UNIQUE constraint failed") ||
		strings.Contains(errMsg, "duplicate key value") ||
		strings.Contains(errMsg, "Duplicate entry")
}
