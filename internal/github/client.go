package github

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v62/github"
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

	// Retry with backoff on rate limit
	for retries := 0; retries < 3; retries++ {
		releases, err = c.fetchReleases(ctx, owner, repo)
		if err == nil {
			return releases, nil
		}

		// Check for rate limit
		if isRateLimitError(err) {
			waitTime := time.Duration(retries+1) * time.Minute
			log.Printf("Rate limited, waiting %v before retry", waitTime)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}

		// Non-rate-limit error, return immediately
		return nil, fmt.Errorf("failed to fetch releases for %s/%s: %w", owner, repo, err)
	}

	return nil, err
}

// fetchReleases fetches releases with pagination
func (c *Client) fetchReleases(ctx context.Context, owner, repo string) ([]ReleaseInfo, error) {
	var releases []ReleaseInfo

	opts := &gh.ListOptions{PerPage: 100}
	for {
		// Add delay between requests to avoid secondary rate limits
		time.Sleep(1 * time.Second)

		ghReleases, resp, err := c.client.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}

		for _, r := range ghReleases {
			release := ReleaseInfo{
				ID:          r.GetID(),
				TagName:     r.GetTagName(),
				Name:        r.GetName(),
				Body:        r.GetBody(),
				HTMLURL:     r.GetHTMLURL(),
				PublishedAt: r.GetPublishedAt().Time,
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
	time.Sleep(1 * time.Second)

	r, _, err := c.client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release for %s/%s: %w", owner, repo, err)
	}

	release := &ReleaseInfo{
		ID:          r.GetID(),
		TagName:     r.GetTagName(),
		Name:        r.GetName(),
		Body:        r.GetBody(),
		HTMLURL:     r.GetHTMLURL(),
		PublishedAt: r.GetPublishedAt().Time,
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
	time.Sleep(1 * time.Second)

	_, _, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("repository %s/%s not accessible: %w", owner, repo, err)
	}
	return nil
}

// isRateLimitError checks if the error is a rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	// Check for rate limit response
	if ghErr, ok := err.(*gh.ErrorResponse); ok {
		if ghErr.Response.StatusCode == http.StatusForbidden {
			return strings.Contains(strings.ToLower(ghErr.Message), "rate limit")
		}
	}

	return false
}
