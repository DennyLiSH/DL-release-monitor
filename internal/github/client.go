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

// API request delay to avoid secondary rate limits
const apiRequestDelay = 1 * time.Second

// Rate limit retry settings
const (
	maxRetries     = 3
	initialBackoff = 1 * time.Second
	maxBackoff     = 5 * time.Minute
	backoffFactor  = 2.0
)

// Client wraps the GitHub API client
type Client struct {
	client *gh.Client
}

// ReleaseInfo represents a simplified release information
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

// AssetInfo represents a simplified asset information
type AssetInfo struct {
	ID          int64
	Name        string
	Size        int64
	DownloadURL string
}

// NewClient creates a new GitHub client
func NewClient(token string) *Client {
	client := gh.NewClient(nil).WithAuthToken(token)
	return &Client{client: client}
}

// GetReleaseList fetches releases for a repository
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
		case <-time.After(apiRequestDelay):
		}

		ghReleases, resp, err := c.client.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}

		for _, r := range ghReleases {
			var publishedAt time.Time
			if r.PublishedAt != nil {
				publishedAt = r.PublishedAt.Time
			}
			release := ReleaseInfo{
				ID:          r.GetID(),
				TagName:     r.GetTagName(),
				Name:        r.GetName(),
				Body:        r.GetBody(),
				HTMLURL:     r.GetHTMLURL(),
				PublishedAt: publishedAt,
				Prerelease:  r.GetPrerelease(),
				Draft:       r.GetDraft(),
			}

			for _, a := range r.Assets {
				release.Assets = append(release.Assets, AssetInfo{
					ID:          a.GetID(),
					Name:        a.GetName(),
					Size:        int64(a.GetSize()),
					DownloadURL: a.GetBrowserDownloadURL(),
				})
			}

			releases = append(releases, release)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return releases, nil
}

// GetLatestRelease fetches the latest release for a repository
func (c *Client) GetLatestRelease(ctx context.Context, owner, repo string) (*ReleaseInfo, error) {
	// Add delay between requests to avoid secondary rate limits (context-aware)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(apiRequestDelay):
	}

	r, _, err := c.client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release for %s/%s: %w", owner, repo, err)
	}

	var publishedAt time.Time
	if r.PublishedAt != nil {
		publishedAt = r.PublishedAt.Time
	}
	release := &ReleaseInfo{
		ID:          r.GetID(),
		TagName:     r.GetTagName(),
		Name:        r.GetName(),
		Body:        r.GetBody(),
		HTMLURL:     r.GetHTMLURL(),
		PublishedAt: publishedAt,
		Prerelease:  r.GetPrerelease(),
		Draft:       r.GetDraft(),
	}

	for _, a := range r.Assets {
		release.Assets = append(release.Assets, AssetInfo{
			ID:          a.GetID(),
			Name:        a.GetName(),
			Size:        int64(a.GetSize()),
			DownloadURL: a.GetBrowserDownloadURL(),
		})
	}

	return release, nil
}

// ValidateRepo checks if a repository exists and is accessible
func (c *Client) ValidateRepo(ctx context.Context, owner, repo string) error {
	// Add delay between requests to avoid secondary rate limits (context-aware)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(apiRequestDelay):
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
