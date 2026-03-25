package github

import (
	"context"
)

// ClientInterface defines the interface for GitHub API operations.
// This interface allows for mocking in tests and decoupling from the concrete implementation.
type ClientInterface interface {
	// GetReleaseList fetches all releases for a repository with pagination.
	// Returns a slice of ReleaseInfo or an error if the request fails.
	GetReleaseList(ctx context.Context, owner, repo string) ([]ReleaseInfo, error)

	// GetLatestRelease fetches the latest non-draft, non-prerelease release for a repository.
	// Returns the latest release info or an error.
	GetLatestRelease(ctx context.Context, owner, repo string) (*ReleaseInfo, error)

	// ValidateRepo checks if a repository exists and is accessible with the current credentials.
	// Returns nil if accessible, or an error describing why access failed.
	ValidateRepo(ctx context.Context, owner, repo string) error
}

// Ensure Client implements ClientInterface
var _ ClientInterface = (*Client)(nil)
