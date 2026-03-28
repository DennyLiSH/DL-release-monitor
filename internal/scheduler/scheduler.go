package scheduler

import (
	"context"
	"fmt"
	"log/slog"
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
	ghClient    github.ClientInterface
	cfg         *config.Config
	storage     storage.Storage // Use interface for flexibility
	notifyMgr   *notify.Manager
	parser      *release.Parser
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex // protects all fields below
	running     bool
	checking    bool // prevents concurrent checkAllRepos
	initialized bool
	initErr     error
}

// New creates a new scheduler
func New(db *gorm.DB, ghClient github.ClientInterface, cfg *config.Config) *Scheduler {
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

	// Access cfg directly since we already hold the lock (getConfig would deadlock)
	cfg := *s.cfg

	// Initialize storage
	var err error
	s.storage, err = storage.NewLocalStorageWithTimeout(cfg.Storage.Local.Path, cfg.Storage.Local.DownloadTimeout)
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
		webhookNotifier, err := notify.NewWebhookNotifierWithTimeout(cfg.Notify.Webhook.URL, cfg.Notify.Webhook.Timeout)
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
		slog.Error("Failed to initialize scheduler", "error", err)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return
	}

	// Start main loop
	s.wg.Add(1)
	go s.run()

	cfg := s.getConfig()
	slog.Info("Scheduler started", "poll_interval_minutes", cfg.GitHub.PollInterval)
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

	slog.Info("Scheduler stopped")
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
		slog.Error("Cannot check now", "error", err)
		return
	}

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1) // Add to WaitGroup while holding lock to prevent race
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Panic recovered in CheckNow", "panic", r)
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
		return fmt.Errorf("scheduler is not running")
	}
	s.wg.Add(1) // Add to WaitGroup while holding lock to prevent race
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Panic recovered in CheckRepoNow", "panic", rec)
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
		slog.Debug("Check already in progress, skipping")
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
		slog.Error("Failed to fetch repos", "error", err)
		return
	}

	slog.Info("Checking repos", "count", len(repos))

	for i := range repos {
		select {
		case <-s.getContext().Done():
			return
		default:
			s.checkRepo(&repos[i])
		}
	}
}

// getContext returns the scheduler's context under read lock protection
func (s *Scheduler) getContext() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
		slog.Error("Failed to fetch releases", "repo", repo.FullName, "error", err)
		return
	}

	// Update last checked time
	if err := s.db.WithContext(ctx).Model(repo).Update("last_checked_at", time.Now()).Error; err != nil {
		slog.Error("Failed to update last_checked_at", "repo", repo.FullName, "error", err)
	}

	slog.Info("Found releases", "repo", repo.FullName, "count", len(releases))

	// Process each release
	for _, ghRelease := range releases {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.processRelease(ctx, repo, ghRelease)
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
		slog.Error("Failed to create release record", "repo", repo.FullName, "error", err)
		return false
	}

	slog.Info("New release", "repo", repo.FullName, "tag", ghRelease.TagName)

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
			slog.Error("Failed to create asset record", "repo", repo.FullName, "error", err)
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
		slog.Error("Some notifications failed", "repo", repo.FullName, "errors", errs)
	}
}

// downloadAsset downloads a single asset
func (s *Scheduler) downloadAsset(ctx context.Context, repo *models.Repo, rel *models.Release, asset *models.Asset) {
	// Mark as downloading
	if err := s.db.Model(asset).Update("status", models.AssetStatusDownloading).Error; err != nil {
		slog.Error("Failed to update asset status to downloading", "repo", repo.FullName, "error", err)
	}

	// Use provided context for cancellation support
	localPath, sha256sum, duration, err := s.storage.Download(ctx, asset.DownloadURL, repo.FullName, asset.Name)
	if err != nil {
		if err := s.db.Model(asset).Updates(map[string]any{
			"status":        models.AssetStatusFailed,
			"error_message": err.Error(),
		}).Error; err != nil {
			slog.Error("Failed to update asset failure status", "repo", repo.FullName, "error", err)
		}
		slog.Error("Failed to download asset", "repo", repo.FullName, "asset", asset.Name, "error", err)

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
			slog.Error("Failed to create download log", "repo", repo.FullName, "error", err)
		}
		return
	}

	// Update asset record
	if err := s.db.Model(asset).Updates(map[string]any{
		"local_path": localPath,
		"sha256":     sha256sum,
		"status":     models.AssetStatusDone,
	}).Error; err != nil {
		slog.Error("Failed to update asset success status", "repo", repo.FullName, "error", err)
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
		slog.Error("Failed to create download log", "repo", repo.FullName, "error", err)
	}

	slog.Info("Downloaded asset", "repo", repo.FullName, "asset", asset.Name, "duration_ms", duration)
}

// getConfig returns a copy of the current config under read lock
func (s *Scheduler) getConfig() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
		slog.Error("Failed to fetch releases for retention", "repo", repo.FullName, "error", err)
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
		slog.Error("Failed to fetch assets for retention", "repo", repo.FullName, "error", err)
		return
	}

	// Determine what to delete
	policy := retention.NewPolicy(maxVersions, keepLastMajor)
	toDelete := policy.DetermineAssetsToDelete(releases, assets)

	// Delete assets
	for _, asset := range toDelete {
		if asset.LocalPath != "" {
			if err := s.storage.Delete(asset.LocalPath); err != nil {
				slog.Error("Failed to delete asset file", "repo", repo.FullName, "asset", asset.Name, "error", err)
			} else {
				slog.Info("Deleted asset (retention policy)", "repo", repo.FullName, "asset", asset.Name)
			}
		}
		if err := s.db.Delete(&asset).Error; err != nil {
			slog.Error("Failed to delete asset record", "repo", repo.FullName, "error", err)
		}
	}

	// Delete releases whose assets were all removed by retention policy.
	// Only delete releases that had assets which were deleted above, not releases that never had assets.
	if len(toDelete) > 0 {
		var deletedReleaseIDs []int64
		for _, asset := range toDelete {
			deletedReleaseIDs = append(deletedReleaseIDs, asset.ReleaseID)
		}
		if err := s.db.Where("repo_id = ? AND id IN ?", repo.ID, deletedReleaseIDs).
			Where("id NOT IN (SELECT DISTINCT release_id FROM assets)").Delete(&models.Release{}).Error; err != nil {
			slog.Error("Failed to delete orphan releases", "repo", repo.FullName, "error", err)
		}
	}
}
