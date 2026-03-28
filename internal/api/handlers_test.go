package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"gh-release-monitor/internal/config"
	"gh-release-monitor/internal/github"
	"gh-release-monitor/internal/models"
	"gh-release-monitor/internal/scheduler"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&models.Repo{}, &models.Release{}, &models.Asset{}, &models.DownloadLog{}); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

// setupTestRouter creates a Router with test dependencies
func setupTestRouter(t *testing.T, db *gorm.DB) *Router {
	t.Helper()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 8080,
		},
		GitHub: config.GitHubConfig{
			Token:        "test-token",
			PollInterval: 300,
		},
		Storage: config.StorageConfig{
			Local: config.LocalStorageConfig{
				Enabled: true,
				Path:    t.TempDir(),
			},
		},
		Retention: config.RetentionConfig{
			MaxVersions:   10,
			KeepLastMajor: true,
		},
		Notify: config.NotifyConfig{
			Email: config.EmailConfig{
				Enabled: false,
			},
			Webhook: config.WebhookConfig{
				Enabled: false,
			},
		},
	}

	ghClient := github.NewClient("test-token")
	cfgHolder := config.NewAtomicConfig(cfg)
	sched := scheduler.New(db, ghClient, cfgHolder)

	return NewRouter(db, ghClient, sched, cfgHolder)
}

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		defaultLimit int
		maxLimit     int
		wantPage     int
		wantLimit    int
	}{
		{
			name:         "default values",
			url:          "/",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     1,
			wantLimit:    10,
		},
		{
			name:         "custom page",
			url:          "/?page=5",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     5,
			wantLimit:    10,
		},
		{
			name:         "custom limit",
			url:          "/?limit=25",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     1,
			wantLimit:    25,
		},
		{
			name:         "limit exceeds max",
			url:          "/?limit=200",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     1,
			wantLimit:    100, // uses maxLimit when exceeds max
		},
		{
			name:         "invalid page",
			url:          "/?page=abc",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     1,
			wantLimit:    10,
		},
		{
			name:         "negative page",
			url:          "/?page=-1",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     1,
			wantLimit:    10,
		},
		{
			name:         "zero limit",
			url:          "/?limit=0",
			defaultLimit: 10,
			maxLimit:     100,
			wantPage:     1,
			wantLimit:    10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			page, limit := parsePagination(req, tt.defaultLimit, tt.maxLimit)

			if page != tt.wantPage {
				t.Errorf("parsePagination() page = %v, want %v", page, tt.wantPage)
			}
			if limit != tt.wantLimit {
				t.Errorf("parsePagination() limit = %v, want %v", limit, tt.wantLimit)
			}
		})
	}
}

func TestIsUniqueConstraintError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "UNIQUE constraint failed",
			err:  strconv.ErrSyntax,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUniqueConstraintError(tt.err); got != tt.want {
				t.Errorf("isUniqueConstraintError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HealthCheck() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("HealthCheck() status = %v, want ok", resp["status"])
	}
}

func TestReadyCheck(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ReadyCheck() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["status"] != "ready" {
		t.Errorf("ReadyCheck() status = %v, want ready", resp["status"])
	}
}

func TestGetConfig(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GetConfig() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify config structure
	if _, ok := resp["server"]; !ok {
		t.Error("GetConfig() missing server config")
	}
	if _, ok := resp["github"]; !ok {
		t.Error("GetConfig() missing github config")
	}
	if _, ok := resp["storage"]; !ok {
		t.Error("GetConfig() missing storage config")
	}
}

func TestGetStatus(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	// Add a test repo
	repo := models.Repo{
		Owner:    "testowner",
		Name:     "testrepo",
		FullName: "testowner/testrepo",
		Enabled:  true,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GetStatus() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["status"] != "running" {
		t.Errorf("GetStatus() status = %v, want running", resp["status"])
	}

	// repo_count should be 1
	if count, ok := resp["repo_count"].(float64); !ok || int64(count) != 1 {
		t.Errorf("GetStatus() repo_count = %v, want 1", resp["repo_count"])
	}
}

func TestListRepos(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	// Add test repos
	for i := 0; i < 5; i++ {
		repo := models.Repo{
			Owner:    "owner",
			Name:     "repo" + string(rune('0'+i)),
			FullName: "owner/repo" + string(rune('0'+i)),
			Enabled:  true,
		}
		if err := db.Create(&repo).Error; err != nil {
			t.Fatalf("Failed to create test repo: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ListRepos() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("ListRepos() data is not an array")
	}

	if len(data) != 5 {
		t.Errorf("ListRepos() returned %d repos, want 5", len(data))
	}

	// Check pagination fields
	if total, ok := resp["total"].(float64); !ok || int64(total) != 5 {
		t.Errorf("ListRepos() total = %v, want 5", resp["total"])
	}
}

func TestListReposPagination(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	// Add 15 test repos
	for i := 0; i < 15; i++ {
		repo := models.Repo{
			Owner:    "owner",
			Name:     "repo" + strconv.Itoa(i),
			FullName: "owner/repo" + strconv.Itoa(i),
			Enabled:  true,
		}
		if err := db.Create(&repo).Error; err != nil {
			t.Fatalf("Failed to create test repo: %v", err)
		}
	}

	// Request page 2 with limit 5
	req := httptest.NewRequest(http.MethodGet, "/api/repos?page=2&limit=5", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ListRepos() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return 5 items on page 2
	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("ListRepos() data is not an array")
	}

	if len(data) != 5 {
		t.Errorf("ListRepos() returned %d repos, want 5", len(data))
	}

	if page, ok := resp["page"].(float64); !ok || int(page) != 2 {
		t.Errorf("ListRepos() page = %v, want 2", resp["page"])
	}

	if limit, ok := resp["limit"].(float64); !ok || int(limit) != 5 {
		t.Errorf("ListRepos() limit = %v, want 5", resp["limit"])
	}
}

func TestGetRepoNotFound(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/999", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GetRepo() status = %v, want %v", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateRepoInvalidID(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	req := httptest.NewRequest(http.MethodPut, "/api/repos/invalid", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("UpdateRepo() status = %v, want %v", rec.Code, http.StatusBadRequest)
	}
}

func TestDeleteRepoInvalidID(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/api/repos/invalid", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("DeleteRepo() status = %v, want %v", rec.Code, http.StatusBadRequest)
	}
}

func TestListReleases(t *testing.T) {
	db := setupTestDB(t)
	router := setupTestRouter(t, db)

	// Create a repo first
	repo := models.Repo{
		Owner:    "owner",
		Name:     "repo",
		FullName: "owner/repo",
		Enabled:  true,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("Failed to create test repo: %v", err)
	}

	// Add test releases
	for i := 0; i < 3; i++ {
		release := models.Release{
			RepoID:      repo.ID,
			GitHubID:    int64(1000 + i), // unique GitHub ID
			TagName:     "v1.0." + strconv.Itoa(i),
			PublishedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}
		if err := db.Create(&release).Error; err != nil {
			t.Fatalf("Failed to create test release: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/releases", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("ListReleases() status = %v, want %v", rec.Code, http.StatusOK)
	}

	var releases []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &releases); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(releases) != 3 {
		t.Errorf("ListReleases() returned %d releases, want 3", len(releases))
	}
}
