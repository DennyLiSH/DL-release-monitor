package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"gh-release-monitor/internal/config"
	"gh-release-monitor/internal/github"
	"gh-release-monitor/internal/models"
	"gh-release-monitor/internal/notify"
	"gh-release-monitor/internal/release"
	"gh-release-monitor/internal/retention"
	"gh-release-monitor/internal/storage"

	"gorm.io/gorm"
)

// Scheduler handles periodic release checking
type Scheduler struct {
	db         *gorm.DB
	ghClient   *github.Client
	cfg        *config.Config
	storage    *storage.LocalStorage
	notifyMgr  *notify.Manager
	parser     *release.Parser
	stopChan   chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
	running    bool
}

// New creates a new scheduler
func New(db *gorm.DB, ghClient *github.Client, cfg *config.Config) *Scheduler {
	return &Scheduler{
		db:       db,
		ghClient: ghClient,
		cfg:      cfg,
		stopChan: make(chan struct{}),
	}
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	// Initialize storage
	var err error
	s.storage, err = storage.NewLocalStorage(s.cfg.Storage.Local.Path)
	if err != nil {
		log.Printf("Failed to initialize storage: %v", err)
		return
	}

	// Initialize notification manager
	s.notifyMgr = notify.NewManager()
	if s.cfg.Notify.Email.Enabled {
		s.notifyMgr.AddNotifier(notify.NewEmailNotifier(
			s.cfg.Notify.Email.SMTPHost,
			s.cfg.Notify.Email.SMTPPort,
			s.cfg.Notify.Email.SMTPUser,
			s.cfg.Notify.Email.SMTPPass,
			s.cfg.Notify.Email.From,
			s.cfg.Notify.Email.To,
		))
	}
	if s.cfg.Notify.Webhook.Enabled {
		s.notifyMgr.AddNotifier(notify.NewWebhookNotifier(s.cfg.Notify.Webhook.URL))
	}

	// Initialize parser
	s.parser = release.NewParser()

	// Start main loop
	s.wg.Add(1)
	go s.run()

	log.Printf("Scheduler started with poll interval: %d minutes", s.cfg.GitHub.PollInterval)
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopChan)
	s.wg.Wait()
	log.Println("Scheduler stopped")
}

// run is the main scheduler loop
func (s *Scheduler) run() {
	defer s.wg.Done()

	// Initial check on start
	s.checkAllRepos()

	ticker := time.NewTicker(time.Duration(s.cfg.GitHub.PollInterval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.checkAllRepos()
		}
	}
}

// CheckNow triggers an immediate check of all repos
func (s *Scheduler) CheckNow() {
	go s.checkAllRepos()
}

// CheckRepoNow triggers an immediate check of a specific repo
func (s *Scheduler) CheckRepoNow(repoID int64) error {
	var repo models.Repo
	if err := s.db.First(&repo, repoID).Error; err != nil {
		return err
	}
	go s.checkRepo(&repo)
	return nil
}

// checkAllRepos checks all enabled repos for new releases
func (s *Scheduler) checkAllRepos() {
	var repos []models.Repo
	if err := s.db.Where("enabled = ?", true).Find(&repos).Error; err != nil {
		log.Printf("Failed to fetch repos: %v", err)
		return
	}

	log.Printf("Checking %d repos...", len(repos))

	for i := range repos {
		select {
		case <-s.stopChan:
			return
		default:
			s.checkRepo(&repos[i])
		}
	}
}

// checkRepo checks a single repo for new releases
func (s *Scheduler) checkRepo(repo *models.Repo) {
	ctx := context.Background()

	// Get releases from GitHub
	releases, err := s.ghClient.GetReleaseList(ctx, repo.Owner, repo.Name)
	if err != nil {
		log.Printf("[%s] Failed to fetch releases: %v", repo.FullName, err)
		return
	}

	// Update last checked time
	s.db.Model(repo).Update("last_checked_at", time.Now())

	log.Printf("[%s] Found %d releases", repo.FullName, len(releases))

	// Process each release
	for _, ghRelease := range releases {
		// Skip drafts
		if ghRelease.Draft {
			continue
		}

		// Check if release already exists
		var existingRelease models.Release
		result := s.db.Where("repo_id = ? AND github_id = ?", repo.ID, ghRelease.ID).First(&existingRelease)
		if result.Error == nil {
			// Release already processed
			continue
		}

		// Parse version
		version, major, minor, patch := s.parser.ParseVersion(ghRelease.TagName)

		// Create release record
		newRelease := models.Release{
			RepoID:      repo.ID,
			GitHubID:    ghRelease.ID,
			TagName:     ghRelease.TagName,
			Version:     version,
			Major:       major,
			Minor:       minor,
			Patch:       patch,
			Prerelease:  ghRelease.Prerelease,
			Draft:       ghRelease.Draft,
			PublishedAt: ghRelease.PublishedAt,
			HTMLURL:     ghRelease.HTMLURL,
			Body:        ghRelease.Body,
		}

		if err := s.db.Create(&newRelease).Error; err != nil {
			log.Printf("[%s] Failed to create release record: %v", repo.FullName, err)
			continue
		}

		log.Printf("[%s] New release: %s", repo.FullName, ghRelease.TagName)

		// Process assets
		var downloadedAssets []string
		for _, ghAsset := range ghRelease.Assets {
			assetType := s.parser.GetAssetType(ghAsset.Name)
			if !s.parser.ShouldDownloadAsset(assetType) {
				continue
			}

			// Create asset record
			asset := models.Asset{
				ReleaseID:   newRelease.ID,
				GitHubID:    ghAsset.ID,
				Name:        ghAsset.Name,
				Type:        assetType,
				Size:        ghAsset.Size,
				DownloadURL: ghAsset.DownloadURL,
				Status:      models.AssetStatusPending,
			}

			if err := s.db.Create(&asset).Error; err != nil {
				log.Printf("[%s] Failed to create asset record: %v", repo.FullName, err)
				continue
			}

			// Download asset
			s.downloadAsset(repo, &newRelease, &asset)
			if asset.Status == models.AssetStatusDone {
				downloadedAssets = append(downloadedAssets, asset.Name)
			}
		}

		// Send notification if assets were downloaded
		if len(downloadedAssets) > 0 && s.notifyMgr != nil {
			s.notifyMgr.Send(&notify.Notification{
				RepoName:   repo.FullName,
				Version:    newRelease.Version,
				AssetNames: downloadedAssets,
				HTMLURL:    newRelease.HTMLURL,
			})
		}
	}

	// Apply retention policy
	s.applyRetentionPolicy(repo)
}

// downloadAsset downloads a single asset
func (s *Scheduler) downloadAsset(repo *models.Repo, rel *models.Release, asset *models.Asset) {
	// Mark as downloading
	s.db.Model(asset).Update("status", models.AssetStatusDownloading)

	localPath, sha256sum, duration, err := s.storage.Download(asset.DownloadURL, repo.FullName, asset.Name)
	if err != nil {
		s.db.Model(asset).Updates(map[string]interface{}{
			"status":        models.AssetStatusFailed,
			"error_message": err.Error(),
		})
		log.Printf("[%s] Failed to download %s: %v", repo.FullName, asset.Name, err)

		// Log failure
		s.db.Create(&models.DownloadLog{
			AssetID:  asset.ID,
			RepoName: repo.FullName,
			Version:  rel.Version,
			FileName: asset.Name,
			FileSize: asset.Size,
			Duration: duration,
			Success:  false,
			Error:    err.Error(),
		})
		return
	}

	// Update asset record
	s.db.Model(asset).Updates(map[string]interface{}{
		"local_path": localPath,
		"sha256":     sha256sum,
		"status":     models.AssetStatusDone,
	})

	// Log success
	s.db.Create(&models.DownloadLog{
		AssetID:  asset.ID,
		RepoName: repo.FullName,
		Version:  rel.Version,
		FileName: asset.Name,
		FileSize: asset.Size,
		Duration: duration,
		Success:  true,
	})

	log.Printf("[%s] Downloaded %s (%d ms)", repo.FullName, asset.Name, duration)
}

// applyRetentionPolicy applies retention policy for a repo
func (s *Scheduler) applyRetentionPolicy(repo *models.Repo) {
	// Get effective retention settings
	maxVersions := s.cfg.GetRetention(repo.Retention)
	keepLastMajor := s.cfg.Retention.KeepLastMajor

	// Get all releases for this repo
	var releases []models.Release
	s.db.Where("repo_id = ?", repo.ID).Order("published_at DESC").Find(&releases)

	if len(releases) <= maxVersions {
		return
	}

	// Get all assets for this repo
	var assets []models.Asset
	s.db.Joins("JOIN releases ON releases.id = assets.release_id").
		Where("releases.repo_id = ?", repo.ID).
		Find(&assets)

	// Determine what to delete
	policy := retention.NewPolicy(maxVersions, keepLastMajor)
	toDelete := policy.DetermineAssetsToDelete(releases, assets)

	// Delete assets
	for _, asset := range toDelete {
		if asset.LocalPath != "" {
			if err := s.storage.Delete(asset.LocalPath); err != nil {
				log.Printf("[%s] Failed to delete %s: %v", repo.FullName, asset.Name, err)
			} else {
				log.Printf("[%s] Deleted %s (retention policy)", repo.FullName, asset.Name)
			}
		}
		s.db.Delete(&asset)
	}

	// Delete releases with no assets
	s.db.Where("repo_id = ? AND id NOT IN (SELECT DISTINCT release_id FROM assets)", repo.ID).Delete(&models.Release{})
}
