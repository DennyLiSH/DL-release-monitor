package scheduler

import (
	"context"
	"fmt"
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
	db          *gorm.DB
	ghClient    *github.Client
	cfg         *config.Config
	cfgMu       sync.RWMutex    // protects cfg access
	storage     storage.Storage // Use interface for flexibility
	notifyMgr   *notify.Manager
	parser      *release.Parser
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex
	running     bool
	checking    bool // prevents concurrent checkAllRepos
	initialized bool
	initErr     error
}

// New creates a new scheduler
func New(db *gorm.DB, ghClient *github.Client, cfg *config.Config) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		db:       db,
		ghClient: ghClient,
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// ensureInit ensures storage, notifyMgr, and parser are initialized
func (s *Scheduler) ensureInit() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return s.initErr
	}

	cfg := s.getConfig()

	// Initialize storage
	var err error
	s.storage, err = storage.NewLocalStorage(cfg.Storage.Local.Path)
	if err != nil {
		s.initErr = fmt.Errorf("failed to initialize storage: %w", err)
		return s.initErr
	}

	// Initialize notification manager
	s.notifyMgr = notify.NewManager()
	if cfg.Notify.Email.Enabled {
		s.notifyMgr.AddNotifier(notify.NewEmailNotifier(
			cfg.Notify.Email.SMTPHost,
			cfg.Notify.Email.SMTPPort,
			cfg.Notify.Email.SMTPUser,
			cfg.Notify.Email.SMTPPass,
			cfg.Notify.Email.From,
			cfg.Notify.Email.To,
			cfg.Notify.Email.UseTLS,
		))
	}
	if cfg.Notify.Webhook.Enabled {
		webhookNotifier, err := notify.NewWebhookNotifier(cfg.Notify.Webhook.URL)
		if err != nil {
			s.initErr = fmt.Errorf("failed to create webhook notifier: %w", err)
			return s.initErr
		}
		s.notifyMgr.AddNotifier(webhookNotifier)
	}

	// Initialize parser
	s.parser = release.NewParser()
	s.initialized = true

	return nil
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	// Reset context to allow restart after Stop()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true
	s.mu.Unlock()

	// Initialize dependencies
	if err := s.ensureInit(); err != nil {
		log.Printf("Failed to initialize scheduler: %v", err)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return
	}

	// Start main loop
	s.wg.Add(1)
	go s.run()

	cfg := s.getConfig()
	log.Printf("Scheduler started with poll interval: %d minutes", cfg.GitHub.PollInterval)
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

	s.cancel()
	s.wg.Wait()

	// Reset initialization state to allow restart with fresh initialization
	s.mu.Lock()
	s.initialized = false
	s.initErr = nil
	s.mu.Unlock()

	log.Println("Scheduler stopped")
}

// run is the main scheduler loop
func (s *Scheduler) run() {
	defer s.wg.Done()

	// Initial check on start
	s.checkAllRepos()

	cfg := s.getConfig()
	ticker := time.NewTicker(time.Duration(cfg.GitHub.PollInterval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.getContext().Done():
			return
		case <-ticker.C:
			s.checkAllRepos()
		}
	}
}

// CheckNow triggers an immediate check of all repos
func (s *Scheduler) CheckNow() {
	// Ensure dependencies are initialized
	if err := s.ensureInit(); err != nil {
		log.Printf("Cannot check now: %v", err)
		return
	}

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic recovered in CheckNow: %v", r)
			}
		}()
		s.checkAllRepos()
	}()
}

// CheckRepoNow triggers an immediate check of a specific repo
func (s *Scheduler) CheckRepoNow(repoID int64) error {
	var repo models.Repo
	if err := s.db.First(&repo, repoID).Error; err != nil {
		return err
	}

	// Ensure dependencies are initialized
	if err := s.ensureInit(); err != nil {
		return fmt.Errorf("cannot check repo: %w", err)
	}

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic recovered in CheckRepoNow: %v", r)
			}
		}()
		s.checkRepo(&repo)
	}()
	return nil
}

// checkAllRepos checks all enabled repos for new releases
func (s *Scheduler) checkAllRepos() {
	s.mu.Lock()
	if s.checking {
		s.mu.Unlock()
		log.Println("Check already in progress, skipping")
		return
	}
	s.checking = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.checking = false
		s.mu.Unlock()
	}()

	var repos []models.Repo
	if err := s.db.Where("enabled = ?", true).Find(&repos).Error; err != nil {
		log.Printf("Failed to fetch repos: %v", err)
		return
	}

	log.Printf("Checking %d repos...", len(repos))

	for i := range repos {
		select {
		case <-s.getContext().Done():
			return
		default:
			s.checkRepo(&repos[i])
		}
	}
}

// getContext returns the scheduler's context under lock protection
func (s *Scheduler) getContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

// checkRepo checks a single repo for new releases
func (s *Scheduler) checkRepo(repo *models.Repo) {
	ctx := s.getContext()

	// Check if scheduler is stopping
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Get releases from GitHub
	releases, err := s.ghClient.GetReleaseList(ctx, repo.Owner, repo.Name)
	if err != nil {
		log.Printf("[%s] Failed to fetch releases: %v", repo.FullName, err)
		return
	}

	// Update last checked time
	if err := s.db.WithContext(ctx).Model(repo).Update("last_checked_at", time.Now()).Error; err != nil {
		log.Printf("[%s] Failed to update last_checked_at: %v", repo.FullName, err)
	}

	log.Printf("[%s] Found %d releases", repo.FullName, len(releases))

	// Process each release
	for _, ghRelease := range releases {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if s.processRelease(ctx, repo, ghRelease) {
			// New release was processed successfully
		}
	}

	// Apply retention policy
	s.applyRetentionPolicy(repo)
}

// processRelease processes a single GitHub release, returns true if it was newly processed
func (s *Scheduler) processRelease(ctx context.Context, repo *models.Repo, ghRelease github.ReleaseInfo) bool {
	// Skip drafts
	if ghRelease.Draft {
		return false
	}

	// Check if release already exists
	var existingRelease models.Release
	result := s.db.WithContext(ctx).Where("repo_id = ? AND github_id = ?", repo.ID, ghRelease.ID).First(&existingRelease)
	if result.Error == nil {
		// Release already processed
		return false
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

	if err := s.db.WithContext(ctx).Create(&newRelease).Error; err != nil {
		log.Printf("[%s] Failed to create release record: %v", repo.FullName, err)
		return false
	}

	log.Printf("[%s] New release: %s", repo.FullName, ghRelease.TagName)

	// Process assets
	downloadedAssets := s.processReleaseAssets(ctx, repo, &newRelease, ghRelease.Assets)

	// Send notification if assets were downloaded
	if len(downloadedAssets) > 0 && s.notifyMgr != nil {
		s.sendNotification(ctx, repo, &newRelease, downloadedAssets)
	}

	return true
}

// processReleaseAssets processes all assets for a release, returns list of downloaded asset names
func (s *Scheduler) processReleaseAssets(ctx context.Context, repo *models.Repo, release *models.Release, assets []github.AssetInfo) []string {
	var downloadedAssets []string

	for _, ghAsset := range assets {
		assetType := s.parser.GetAssetType(ghAsset.Name)
		if !s.parser.ShouldDownloadAsset(assetType) {
			continue
		}

		// Create asset record
		asset := models.Asset{
			ReleaseID:   release.ID,
			GitHubID:    ghAsset.ID,
			Name:        ghAsset.Name,
			Type:        assetType,
			Size:        ghAsset.Size,
			DownloadURL: ghAsset.DownloadURL,
			Status:      models.AssetStatusPending,
		}

		if err := s.db.WithContext(ctx).Create(&asset).Error; err != nil {
			log.Printf("[%s] Failed to create asset record: %v", repo.FullName, err)
			continue
		}

		// Download asset
		s.downloadAsset(ctx, repo, release, &asset)
		if asset.Status == models.AssetStatusDone {
			downloadedAssets = append(downloadedAssets, asset.Name)
		}
	}

	return downloadedAssets
}

// sendNotification sends a notification for a new release
func (s *Scheduler) sendNotification(ctx context.Context, repo *models.Repo, release *models.Release, assetNames []string) {
	errs := s.notifyMgr.Send(ctx, &notify.Notification{
		RepoName:   repo.FullName,
		Version:    release.Version,
		AssetNames: assetNames,
		HTMLURL:    release.HTMLURL,
	})
	if len(errs) > 0 {
		log.Printf("[%s] Some notifications failed: %v", repo.FullName, errs)
	}
}

// downloadAsset downloads a single asset
func (s *Scheduler) downloadAsset(ctx context.Context, repo *models.Repo, rel *models.Release, asset *models.Asset) {
	// Mark as downloading
	if err := s.db.Model(asset).Update("status", models.AssetStatusDownloading).Error; err != nil {
		log.Printf("[%s] Failed to update asset status to downloading: %v", repo.FullName, err)
	}

	// Use provided context for cancellation support
	localPath, sha256sum, duration, err := s.storage.Download(ctx, asset.DownloadURL, repo.FullName, asset.Name)
	if err != nil {
		if err := s.db.Model(asset).Updates(map[string]any{
			"status":        models.AssetStatusFailed,
			"error_message": err.Error(),
		}).Error; err != nil {
			log.Printf("[%s] Failed to update asset failure status: %v", repo.FullName, err)
		}
		log.Printf("[%s] Failed to download %s: %v", repo.FullName, asset.Name, err)

		// Log failure
		if err := s.db.Create(&models.DownloadLog{
			AssetID:  asset.ID,
			RepoName: repo.FullName,
			Version:  rel.Version,
			FileName: asset.Name,
			FileSize: asset.Size,
			Duration: duration,
			Success:  false,
			Error:    err.Error(),
		}).Error; err != nil {
			log.Printf("[%s] Failed to create download log: %v", repo.FullName, err)
		}
		return
	}

	// Update asset record
	if err := s.db.Model(asset).Updates(map[string]any{
		"local_path": localPath,
		"sha256":     sha256sum,
		"status":     models.AssetStatusDone,
	}).Error; err != nil {
		log.Printf("[%s] Failed to update asset success status: %v", repo.FullName, err)
	}

	// Log success
	if err := s.db.Create(&models.DownloadLog{
		AssetID:  asset.ID,
		RepoName: repo.FullName,
		Version:  rel.Version,
		FileName: asset.Name,
		FileSize: asset.Size,
		Duration: duration,
		Success:  true,
	}).Error; err != nil {
		log.Printf("[%s] Failed to create download log: %v", repo.FullName, err)
	}

	log.Printf("[%s] Downloaded %s (%d ms)", repo.FullName, asset.Name, duration)
}

// getConfig returns a copy of the current config under read lock
func (s *Scheduler) getConfig() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return *s.cfg
}

// applyRetentionPolicy applies retention policy for a repo
func (s *Scheduler) applyRetentionPolicy(repo *models.Repo) {
	// Get effective retention settings
	cfg := s.getConfig()
	maxVersions := cfg.GetRetention(repo.Retention)
	keepLastMajor := cfg.Retention.KeepLastMajor

	// Get all releases for this repo
	var releases []models.Release
	if err := s.db.Where("repo_id = ?", repo.ID).Order("published_at DESC").Find(&releases).Error; err != nil {
		log.Printf("[%s] Failed to fetch releases for retention: %v", repo.FullName, err)
		return
	}

	if len(releases) <= maxVersions {
		return
	}

	// Get all assets for this repo
	var assets []models.Asset
	if err := s.db.Joins("JOIN releases ON releases.id = assets.release_id").
		Where("releases.repo_id = ?", repo.ID).
		Find(&assets).Error; err != nil {
		log.Printf("[%s] Failed to fetch assets for retention: %v", repo.FullName, err)
		return
	}

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
		if err := s.db.Delete(&asset).Error; err != nil {
			log.Printf("[%s] Failed to delete asset record: %v", repo.FullName, err)
		}
	}

	// Delete releases with no assets
	if err := s.db.Where("repo_id = ? AND id NOT IN (SELECT DISTINCT release_id FROM assets)", repo.ID).Delete(&models.Release{}).Error; err != nil {
		log.Printf("[%s] Failed to delete orphan releases: %v", repo.FullName, err)
	}
}
