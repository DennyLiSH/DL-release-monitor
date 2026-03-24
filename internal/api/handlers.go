package api

import (
	"encoding/json"
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

// ListRepos returns all repositories
func (r *Router) ListRepos(w http.ResponseWriter, req *http.Request) {
	var repos []models.Repo
	if err := r.db.Order("created_at DESC").Find(&repos).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	r.writeJSON(w, http.StatusOK, repos)
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
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
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
		if err == gorm.ErrRecordNotFound {
			r.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Load releases
	if err := r.db.Where("repo_id = ?", repo.ID).Order("published_at DESC").Limit(10).Find(&repo.Releases).Error; err != nil {
		log.Printf("Failed to load releases for repo %d: %v", repo.ID, err)
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
		if err == gorm.ErrRecordNotFound {
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

	updates := map[string]interface{}{}
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
		if err == gorm.ErrRecordNotFound {
			r.writeError(w, http.StatusNotFound, "Repository not found")
			return
		}
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Delete associated assets' files (before transaction)
	var assets []models.Asset
	if err := r.db.Joins("JOIN releases ON releases.id = assets.release_id").
		Where("releases.repo_id = ?", repo.ID).
		Find(&assets).Error; err != nil {
		log.Printf("Failed to fetch assets for deletion: %v", err)
	}

	storageBackend, err := storage.NewLocalStorage(r.cfg.Storage.Local.Path)
	if err != nil {
		log.Printf("Failed to initialize storage for deletion: %v", err)
	} else {
		for _, asset := range assets {
			if asset.LocalPath != "" {
				if err := storageBackend.Delete(asset.LocalPath); err != nil {
					log.Printf("Failed to delete asset file %s: %v", asset.LocalPath, err)
				}
			}
		}
	}

	// Delete from database with transaction to ensure atomicity
	err = r.db.Transaction(func(tx *gorm.DB) error {
		// Delete releases first (assets will cascade)
		if err := tx.Where("repo_id = ?", repo.ID).Delete(&models.Release{}).Error; err != nil {
			return fmt.Errorf("failed to delete releases: %w", err)
		}
		// Then delete the repo
		if err := tx.Delete(&repo).Error; err != nil {
			return fmt.Errorf("failed to delete repository: %w", err)
		}
		return nil
	})

	if err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

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
	if err := query.Limit(100).Find(&releases).Error; err != nil {
		r.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	r.writeJSON(w, http.StatusOK, releases)
}

// ListDownloads returns download history
func (r *Router) ListDownloads(w http.ResponseWriter, req *http.Request) {
	var logs []models.DownloadLog
	if err := r.db.Order("created_at DESC").Limit(100).Find(&logs).Error; err != nil {
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
		r.writeError(w, http.StatusNotFound, "Repository not found")
		return
	}

	r.writeJSON(w, http.StatusOK, map[string]string{"message": "Check triggered"})
}

// GetConfig returns current configuration
func (r *Router) GetConfig(w http.ResponseWriter, req *http.Request) {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()

	// Return sanitized config (no secrets)
	cfg := map[string]interface{}{
		"server": map[string]interface{}{
			"port":     r.cfg.Server.Port,
			"base_url": r.cfg.Server.BaseURL,
		},
		"github": map[string]interface{}{
			"poll_interval": r.cfg.GitHub.PollInterval,
		},
		"storage": map[string]interface{}{
			"local": map[string]interface{}{
				"enabled": r.cfg.Storage.Local.Enabled,
				"path":    r.cfg.Storage.Local.Path,
			},
		},
		"retention": map[string]interface{}{
			"max_versions":    r.cfg.Retention.MaxVersions,
			"keep_last_major": r.cfg.Retention.KeepLastMajor,
		},
		"notify": map[string]interface{}{
			"email": map[string]interface{}{
				"enabled": r.cfg.Notify.Email.Enabled,
			},
			"webhook": map[string]interface{}{
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
	var repoCount int64
	var releaseCount int64
	var assetCount int64
	var downloadCount int64

	if err := r.db.Model(&models.Repo{}).Count(&repoCount).Error; err != nil {
		log.Printf("Failed to count repos: %v", err)
	}
	if err := r.db.Model(&models.Release{}).Count(&releaseCount).Error; err != nil {
		log.Printf("Failed to count releases: %v", err)
	}
	if err := r.db.Model(&models.Asset{}).Where("status = ?", models.AssetStatusDone).Count(&assetCount).Error; err != nil {
		log.Printf("Failed to count assets: %v", err)
	}
	if err := r.db.Model(&models.DownloadLog{}).Where("success = ?", true).Count(&downloadCount).Error; err != nil {
		log.Printf("Failed to count downloads: %v", err)
	}

	// Calculate storage usage
	var storageSize int64
	if err := r.db.Model(&models.Asset{}).
		Where("status = ?", models.AssetStatusDone).
		Select("COALESCE(SUM(size), 0)").Row().Scan(&storageSize); err != nil {
		log.Printf("Failed to calculate storage size: %v", err)
	}

	status := map[string]interface{}{
		"status":         "running",
		"uptime":         time.Since(r.startTime).String(),
		"repo_count":     repoCount,
		"release_count":  releaseCount,
		"asset_count":    assetCount,
		"download_count": downloadCount,
		"storage_size":   storageSize,
	}

	r.writeJSON(w, http.StatusOK, status)
}

// writeJSON writes JSON response
func (r *Router) writeJSON(w http.ResponseWriter, status int, data interface{}) {
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
