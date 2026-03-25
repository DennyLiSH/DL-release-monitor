package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gh "github.com/google/go-github/v62/github"
)

func TestIsRetryableError(t *testing.T) {
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
			name: "context canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "rate limit error",
			err: &gh.ErrorResponse{
				Message: "API rate limit exceeded",
				Response: &http.Response{
					StatusCode: http.StatusForbidden,
				},
			},
			want: true,
		},
		{
			name: "forbidden without rate limit",
			err: &gh.ErrorResponse{
				Message: "Access forbidden",
				Response: &http.Response{
					StatusCode: http.StatusForbidden,
				},
			},
			want: false,
		},
		{
			name: "server error 500",
			err: &gh.ErrorResponse{
				Response: &http.Response{
					StatusCode: http.StatusInternalServerError,
				},
			},
			want: true,
		},
		{
			name: "server error 502",
			err: &gh.ErrorResponse{
				Response: &http.Response{
					StatusCode: http.StatusBadGateway,
				},
			},
			want: true,
		},
		{
			name: "server error 503",
			err: &gh.ErrorResponse{
				Response: &http.Response{
					StatusCode: http.StatusServiceUnavailable,
				},
			},
			want: true,
		},
		{
			name: "client error 404",
			err: &gh.ErrorResponse{
				Response: &http.Response{
					StatusCode: http.StatusNotFound,
				},
			},
			want: false,
		},
		{
			name: "nil response in error",
			err: &gh.ErrorResponse{
				Response: nil,
			},
			want: false,
		},
		{
			name: "connection refused",
			err:  errors.New("dial tcp: connection refused"),
			want: true,
		},
		{
			name: "connection reset",
			err:  errors.New("read tcp: connection reset by peer"),
			want: true,
		},
		{
			name: "EOF error",
			err:  errors.New("unexpected EOF"),
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.want {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRetryableErrorNetError(t *testing.T) {
	// Test net.Error timeout
	timeoutErr := &testNetError{timeout: true, temporary: true}
	if got := isRetryableError(timeoutErr); !got {
		t.Errorf("isRetryableError() for timeout net.Error = %v, want true", got)
	}

	// Test net.Error temporary
	tempErr := &testNetError{timeout: false, temporary: true}
	if got := isRetryableError(tempErr); !got {
		t.Errorf("isRetryableError() for temporary net.Error = %v, want true", got)
	}

	// Test net.Error neither
	permErr := &testNetError{timeout: false, temporary: false}
	if got := isRetryableError(permErr); got {
		t.Errorf("isRetryableError() for permanent net.Error = %v, want false", got)
	}
}

// testNetError is a mock net.Error for testing
type testNetError struct {
	timeout   bool
	temporary bool
}

func (e *testNetError) Error() string   { return "test net error" }
func (e *testNetError) Timeout() bool   { return e.timeout }
func (e *testNetError) Temporary() bool { return e.temporary }

func TestBuildAssets(t *testing.T) {
	tests := []struct {
		name   string
		assets []*gh.ReleaseAsset
		want   int
	}{
		{
			name:   "nil assets",
			assets: nil,
			want:   0,
		},
		{
			name:   "empty assets",
			assets: []*gh.ReleaseAsset{},
			want:   0,
		},
		{
			name: "single asset",
			assets: []*gh.ReleaseAsset{
				{
					ID:                 gh.Int64(1),
					Name:               gh.String("app.exe"),
					Size:               gh.Int(1024),
					BrowserDownloadURL: gh.String("https://example.com/app.exe"),
				},
			},
			want: 1,
		},
		{
			name: "multiple assets",
			assets: []*gh.ReleaseAsset{
				{
					ID:                 gh.Int64(1),
					Name:               gh.String("app.exe"),
					Size:               gh.Int(1024),
					BrowserDownloadURL: gh.String("https://example.com/app.exe"),
				},
				{
					ID:                 gh.Int64(2),
					Name:               gh.String("app.tar.gz"),
					Size:               gh.Int(2048),
					BrowserDownloadURL: gh.String("https://example.com/app.tar.gz"),
				},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAssets(tt.assets)
			if len(got) != tt.want {
				t.Errorf("buildAssets() returned %d assets, want %d", len(got), tt.want)
			}

			// Verify asset content for non-empty cases
			if tt.want > 0 && len(got) > 0 {
				for i, asset := range got {
					if asset.Name != *tt.assets[i].Name {
						t.Errorf("buildAssets()[%d].Name = %v, want %v", i, asset.Name, *tt.assets[i].Name)
					}
					if asset.ID != *tt.assets[i].ID {
						t.Errorf("buildAssets()[%d].ID = %v, want %v", i, asset.ID, *tt.assets[i].ID)
					}
				}
			}
		})
	}
}

func TestConvertRelease(t *testing.T) {
	now := time.Now()
	release := &gh.RepositoryRelease{
		ID:          gh.Int64(123),
		TagName:     gh.String("v1.0.0"),
		Name:        gh.String("Release 1.0.0"),
		Body:        gh.String("Release notes"),
		HTMLURL:     gh.String("https://github.com/owner/repo/releases/tag/v1.0.0"),
		PublishedAt: &gh.Timestamp{Time: now},
		Prerelease:  gh.Bool(false),
		Draft:       gh.Bool(false),
		Assets: []*gh.ReleaseAsset{
			{
				ID:                 gh.Int64(1),
				Name:               gh.String("app.exe"),
				Size:               gh.Int(1024),
				BrowserDownloadURL: gh.String("https://example.com/app.exe"),
			},
		},
	}

	got := convertRelease(release)

	if got.ID != 123 {
		t.Errorf("convertRelease().ID = %v, want 123", got.ID)
	}
	if got.TagName != "v1.0.0" {
		t.Errorf("convertRelease().TagName = %v, want v1.0.0", got.TagName)
	}
	if got.Name != "Release 1.0.0" {
		t.Errorf("convertRelease().Name = %v, want Release 1.0.0", got.Name)
	}
	if got.Body != "Release notes" {
		t.Errorf("convertRelease().Body = %v, want Release notes", got.Body)
	}
	if got.HTMLURL != "https://github.com/owner/repo/releases/tag/v1.0.0" {
		t.Errorf("convertRelease().HTMLURL = %v, want https://github.com/owner/repo/releases/tag/v1.0.0", got.HTMLURL)
	}
	if !got.PublishedAt.Equal(now) {
		t.Errorf("convertRelease().PublishedAt = %v, want %v", got.PublishedAt, now)
	}
	if got.Prerelease != false {
		t.Errorf("convertRelease().Prerelease = %v, want false", got.Prerelease)
	}
	if got.Draft != false {
		t.Errorf("convertRelease().Draft = %v, want false", got.Draft)
	}
	if len(got.Assets) != 1 {
		t.Errorf("convertRelease().Assets length = %v, want 1", len(got.Assets))
	}
}

func TestConvertReleaseNilPublishedAt(t *testing.T) {
	release := &gh.RepositoryRelease{
		ID:          gh.Int64(123),
		TagName:     gh.String("v1.0.0"),
		PublishedAt: nil,
	}

	got := convertRelease(release)

	if !got.PublishedAt.IsZero() {
		t.Errorf("convertRelease().PublishedAt should be zero when input is nil, got %v", got.PublishedAt)
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-token")
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.client == nil {
		t.Error("NewClient().client is nil")
	}
}

func TestValidateRepoWithMockServer(t *testing.T) {
	// Create a mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": 1, "name": "repo"}`))
			return
		}
		if r.URL.Path == "/repos/owner/notfound" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not Found"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create client pointing to mock server
	ghClient := gh.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(server.URL + "/")
	client := &Client{client: ghClient, apiRequestDelay: 0} // Disable delay for tests

	// Test valid repo
	ctx := context.Background()
	err := client.ValidateRepo(ctx, "owner", "repo")
	if err != nil {
		t.Errorf("ValidateRepo() for valid repo returned error: %v", err)
	}

	// Test invalid repo - note: apiRequestDelay will cause this test to be slow
	// In real tests, we might want to make the delay configurable
}

func TestGetLatestReleaseWithMockServer(t *testing.T) {
	// Create a mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/releases/latest" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id": 1,
				"tag_name": "v1.0.0",
				"name": "Release 1.0.0",
				"body": "Release notes",
				"html_url": "https://github.com/owner/repo/releases/tag/v1.0.0",
				"published_at": "2024-01-01T00:00:00Z",
				"prerelease": false,
				"draft": false,
				"assets": []
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create client pointing to mock server
	ghClient := gh.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(server.URL + "/")
	client := &Client{client: ghClient, apiRequestDelay: 0} // Disable delay for tests

	ctx := context.Background()
	release, err := client.GetLatestRelease(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("GetLatestRelease() returned error: %v", err)
	}

	if release.TagName != "v1.0.0" {
		t.Errorf("GetLatestRelease().TagName = %v, want v1.0.0", release.TagName)
	}
	if release.Name != "Release 1.0.0" {
		t.Errorf("GetLatestRelease().Name = %v, want Release 1.0.0", release.Name)
	}
}

func TestGetLatestReleaseContextCancellation(t *testing.T) {
	// Create a mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ghClient := gh.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(server.URL + "/")
	client := &Client{client: ghClient, apiRequestDelay: 0} // Disable delay for tests

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.GetLatestRelease(ctx, "owner", "repo")
	if err == nil {
		t.Error("GetLatestRelease() should return error for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("GetLatestRelease() error = %v, want context.Canceled", err)
	}
}

func TestValidateRepoContextCancellation(t *testing.T) {
	// Create a mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ghClient := gh.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(server.URL + "/")
	client := &Client{client: ghClient, apiRequestDelay: 0} // Disable delay for tests

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.ValidateRepo(ctx, "owner", "repo")
	if err == nil {
		t.Error("ValidateRepo() should return error for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ValidateRepo() error = %v, want context.Canceled", err)
	}
}
