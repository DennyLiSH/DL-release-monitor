package github

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v62/github"
)

// Default API request delay to avoid secondary rate limits
const defaultAPIDelay = 1 * time.Second

// Rate limit retry settings
const (
	maxRetries     = 3
	initialBackoff = 1 * time.Second
	maxBackoff     = 5 * time.Minute
	backoffFactor  = 2.0
)

// Client wraps the GitHub API client with retry logic and rate limiting.
type Client struct {
	client        *gh.Client
	apiRequestDelay time.Duration
}

// ReleaseInfo represents a simplified release information from GitHub.
type ReleaseInfo struct {
	ID          int64
	TagName     string
	Name        string
	Body        string
	HTMLURL     string
	PublishedAt time.Time
	Prerelease  bool
	Draft       bool
	Assets      []AssetInfo
}

// AssetInfo represents a simplified asset information from a GitHub release.
type AssetInfo struct {
	ID          int64
	Name        string
	Size        int64
	DownloadURL string
}

// NewClient creates a new GitHub API client with the provided authentication token.
// The token should be a GitHub personal access token with appropriate repository permissions.
func NewClient(token string) *Client {
	client := gh.NewClient(nil).WithAuthToken(token)
	return &Client{
		client:          client,
		apiRequestDelay: defaultAPIDelay,
	}
}

// SetAPIDelay sets the delay between API requests to avoid secondary rate limits.
// This is useful for testing or for environments with different rate limit requirements.
func (c *Client) SetAPIDelay(delay time.Duration) {
	c.apiRequestDelay = delay
}

// GetReleaseList fetches all releases for a repository with pagination.
// It implements exponential backoff retry logic for rate limits and transient errors.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - owner: Repository owner (e.g., "golang")
//   - repo: Repository name (e.g., "go")
//
// Returns a slice of ReleaseInfo or an error if the request fails after retries.
func (c *Client) GetReleaseList(ctx context.Context, owner, repo string) ([]ReleaseInfo, error) {
	var releases []ReleaseInfo
	var err error

	backoff := initialBackoff
	for retries := 0; retries < maxRetries; retries++ {
		releases, err = c.fetchReleases(ctx, owner, repo)
		if err == nil {
			return releases, nil
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			return nil, fmt.Errorf("failed to fetch releases for %s/%s: %w", owner, repo, err)
		}

		log.Printf("Retryable error for %s/%s, waiting %v before retry %d/%d: %v",
			owner, repo, backoff, retries+1, maxRetries, err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			// Exponential backoff with cap
			backoff = time.Duration(float64(backoff) * backoffFactor)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}

	return nil, fmt.Errorf("failed to fetch releases for %s/%s after %d retries: %w", owner, repo, maxRetries, err)
}

// fetchReleases fetches releases with pagination
func (c *Client) fetchReleases(ctx context.Context, owner, repo string) ([]ReleaseInfo, error) {
	var releases []ReleaseInfo

	opts := &gh.ListOptions{PerPage: 100}
	for {
		// Add delay between requests to avoid secondary rate limits (context-aware)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.apiRequestDelay):
		}

		ghReleases, resp, err := c.client.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}

		for _, r := range ghReleases {
			releases = append(releases, convertRelease(r))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return releases, nil
}

// GetLatestRelease fetches the latest non-draft, non-prerelease release for a repository.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - owner: Repository owner (e.g., "golang")
//   - repo: Repository name (e.g., "go")
//
// Returns the latest release info or an error. Returns error if no releases exist.
func (c *Client) GetLatestRelease(ctx context.Context, owner, repo string) (*ReleaseInfo, error) {
	// Add delay between requests to avoid secondary rate limits (context-aware)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.apiRequestDelay):
	}

	r, _, err := c.client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release for %s/%s: %w", owner, repo, err)
	}

	release := convertRelease(r)
	return &release, nil
}

// convertRelease converts a GitHub release to our ReleaseInfo type
func convertRelease(r *gh.RepositoryRelease) ReleaseInfo {
	var publishedAt time.Time
	if r.PublishedAt != nil {
		publishedAt = r.PublishedAt.Time
	}

	return ReleaseInfo{
		ID:          r.GetID(),
		TagName:     r.GetTagName(),
		Name:        r.GetName(),
		Body:        r.GetBody(),
		HTMLURL:     r.GetHTMLURL(),
		PublishedAt: publishedAt,
		Prerelease:  r.GetPrerelease(),
		Draft:       r.GetDraft(),
		Assets:      buildAssets(r.Assets),
	}
}

// buildAssets converts GitHub assets to our AssetInfo slice
func buildAssets(assets []*gh.ReleaseAsset) []AssetInfo {
	if len(assets) == 0 {
		return nil
	}

	result := make([]AssetInfo, 0, len(assets))
	for _, a := range assets {
		result = append(result, AssetInfo{
			ID:          a.GetID(),
			Name:        a.GetName(),
			Size:        int64(a.GetSize()),
			DownloadURL: a.GetBrowserDownloadURL(),
		})
	}
	return result
}

// ValidateRepo checks if a repository exists and is accessible with the current credentials.
// Returns nil if accessible, or an error describing why access failed.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - owner: Repository owner (e.g., "golang")
//   - repo: Repository name (e.g., "go")
func (c *Client) ValidateRepo(ctx context.Context, owner, repo string) error {
	// Add delay between requests to avoid secondary rate limits (context-aware)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.apiRequestDelay):
	}

	_, _, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("repository %s/%s not accessible: %w", owner, repo, err)
	}
	return nil
}

// isRetryableError checks if the error is retryable (rate limit, network error, 5xx)
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for network errors (timeout, temporary, connection refused)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	// Check for GitHub API rate limit response
	if ghErr, ok := err.(*gh.ErrorResponse); ok {
		// Safely check Response to avoid nil pointer dereference
		if ghErr.Response == nil {
			return false
		}
		// Rate limit (403 with rate limit message)
		if ghErr.Response.StatusCode == http.StatusForbidden {
			return strings.Contains(strings.ToLower(ghErr.Message), "rate limit")
		}
		// Server errors (5xx) are retryable
		if ghErr.Response.StatusCode >= 500 && ghErr.Response.StatusCode < 600 {
			return true
		}
	}

	// Check for connection errors
	if strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connection reset") ||
		strings.Contains(err.Error(), "EOF") {
		return true
	}

	return false
}
