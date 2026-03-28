package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gh-release-monitor/internal/config"
	"gh-release-monitor/internal/github"
	"gh-release-monitor/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Auto migrate
	if err := db.AutoMigrate(&models.Repo{}, &models.Release{}, &models.Asset{}, &models.DownloadLog{}); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

// mockGitHubClient is a mock implementation of GitHub client
type mockGitHubClient struct {
	releases []github.ReleaseInfo
	err      error
	called   bool
	mu       sync.Mutex
}

func (m *mockGitHubClient) GetReleaseList(ctx context.Context, owner, repo string) ([]github.ReleaseInfo, error) {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()
	return m.releases, m.err
}

func (m *mockGitHubClient) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func (m *mockGitHubClient) reset() {
	m.mu.Lock()
	m.called = false
	m.mu.Unlock()
}

// testConfig creates a test configuration
func testConfig(tempDir string) *config.Config {
	return &config.Config{
		GitHub: config.GitHubConfig{
			PollInterval: 1, // 1 minute
		},
		Storage: config.StorageConfig{
			Local: config.LocalStorageConfig{
				Enabled: true,
				Path:    tempDir,
			},
		},
		Retention: config.RetentionConfig{
			MaxVersions:   10,
			KeepLastMajor: true,
		},
	}
}

func TestScheduler_New(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)

	if sched == nil {
		t.Fatal("Expected non-nil scheduler")
	}
	if sched.running {
		t.Error("Expected scheduler to not be running initially")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)

	// Start
	sched.Start()
	if !sched.running {
		t.Error("Expected scheduler to be running after Start()")
	}

	// Double start should be no-op
	sched.Start()
	if !sched.running {
		t.Error("Expected scheduler to still be running after double Start()")
	}

	// Stop
	sched.Stop()
	if sched.running {
		t.Error("Expected scheduler to not be running after Stop()")
	}

	// Double stop should be no-op
	sched.Stop()
	if sched.running {
		t.Error("Expected scheduler to still not be running after double Stop()")
	}
}

func TestScheduler_RestartAfterStop(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)

	// Start and stop
	sched.Start()
	time.Sleep(50 * time.Millisecond)
	sched.Stop()

	// Should be able to restart
	sched.Start()
	if !sched.running {
		t.Error("Expected scheduler to be running after restart")
	}
	sched.Stop()
}

func TestScheduler_CheckNowNotRunning(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)

	// CheckNow without starting should not panic
	sched.CheckNow()
	// Just verify it doesn't panic
}

func TestScheduler_CheckRepoNowNotFound(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)
	sched.Start()
	defer sched.Stop()

	// Check non-existent repo
	err := sched.CheckRepoNow(999)
	if err == nil {
		t.Error("Expected error for non-existent repo")
	}
}

func TestScheduler_CheckRepoNotRunning(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	// Create a test repo
	repo := models.Repo{
		Owner:    "test",
		Name:     "repo",
		FullName: "test/repo",
		Enabled:  true,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}

	sched := New(db, ghClient, cfg)
	// Don't start the scheduler

	// Should return error when not running
	err := sched.CheckRepoNow(repo.ID)
	if err == nil {
		t.Error("Expected error when scheduler not running")
	}
}

func TestScheduler_ConcurrentCheckPrevention(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	// Create test repos with unique names
	for i := 0; i < 3; i++ {
		repo := models.Repo{
			Owner:    "test",
			Name:     fmt.Sprintf("repo%d", i),
			FullName: fmt.Sprintf("test/repo%d", i),
			Enabled:  true,
		}
		if err := db.Create(&repo).Error; err != nil {
			t.Fatalf("Failed to create test repo: %v", err)
		}
	}

	sched := New(db, ghClient, cfg)
	sched.Start()

	// Wait for initial check to complete
	time.Sleep(100 * time.Millisecond)

	// Trigger multiple CheckNow concurrently - this should not panic or race
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sched.CheckNow()
		}()
	}
	wg.Wait()

	// Wait for any pending check to complete before stopping
	time.Sleep(100 * time.Millisecond)

	// Stop the scheduler
	sched.Stop()

	// Test passes if we reach here without panic or race
}

func TestScheduler_GetConfig(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	cfg.GitHub.PollInterval = 5
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)

	// getConfig should return a copy
	gotCfg := sched.getConfig()
	if gotCfg.GitHub.PollInterval != 5 {
		t.Errorf("Expected poll interval 5, got %d", gotCfg.GitHub.PollInterval)
	}
}

func TestScheduler_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig(t.TempDir())
	ghClient := github.NewClient("test-token")
	ghClient.SetAPIDelay(0) // Disable delay for tests

	sched := New(db, ghClient, cfg)
	sched.Start()

	// Cancel context and verify Stop works
	ctx := sched.getContext()
	if ctx == nil {
		t.Error("Expected non-nil context")
	}

	// Stop should cancel the context
	sched.Stop()

	// After stop, the old context should be done
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Expected context to be cancelled after Stop()")
	}
}
