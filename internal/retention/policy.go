package retention

import (
	"sort"

	"gh-release-monitor/internal/models"
)

// Policy handles retention policy logic
type Policy struct {
	MaxVersions   int
	KeepLastMajor bool
}

// NewPolicy creates a new retention policy
func NewPolicy(maxVersions int, keepLastMajor bool) *Policy {
	return &Policy{
		MaxVersions:   maxVersions,
		KeepLastMajor: keepLastMajor,
	}
}

// sortReleasesByVersion sorts releases by semantic version (newest first)
func sortReleasesByVersion(releases []models.Release) []models.Release {
	sorted := make([]models.Release, len(releases))
	copy(sorted, releases)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Major != sorted[j].Major {
			return sorted[i].Major > sorted[j].Major
		}
		if sorted[i].Minor != sorted[j].Minor {
			return sorted[i].Minor > sorted[j].Minor
		}
		return sorted[i].Patch > sorted[j].Patch
	})

	return sorted
}

// versionInfo holds precomputed version information for retention decisions
type versionInfo struct {
	highestMinorForMajor map[int]int
	highestPatchForMinor map[[2]int]int
}

// calculateVersionInfo precomputes version information for retention decisions
func calculateVersionInfo(sortedReleases []models.Release) versionInfo {
	info := versionInfo{
		highestMinorForMajor: make(map[int]int),
		highestPatchForMinor: make(map[[2]int]int),
	}

	for _, r := range sortedReleases {
		if highest, exists := info.highestMinorForMajor[r.Major]; !exists || r.Minor > highest {
			info.highestMinorForMajor[r.Major] = r.Minor
		}

		key := [2]int{r.Major, r.Minor}
		if highest, exists := info.highestPatchForMinor[key]; !exists || r.Patch > highest {
			info.highestPatchForMinor[key] = r.Patch
		}
	}

	return info
}

// determineKeepReleases determines which releases to keep based on policy
func (p *Policy) determineKeepReleases(sortedReleases []models.Release, info versionInfo) map[int64]bool {
	keepReleases := make(map[int64]bool)

	for i, r := range sortedReleases {
		// Keep if within max versions
		if i < p.MaxVersions {
			keepReleases[r.ID] = true
			continue
		}

		// Keep if it's the last of its major version
		if p.KeepLastMajor {
			key := [2]int{r.Major, r.Minor}
			// Keep if this is the highest patch for this major.minor
			if r.Patch == info.highestPatchForMinor[key] && r.Minor == info.highestMinorForMajor[r.Major] {
				keepReleases[r.ID] = true
			}
		}
	}

	return keepReleases
}

// DetermineAssetsToDelete determines which assets should be deleted based on retention policy
func (p *Policy) DetermineAssetsToDelete(releases []models.Release, assets []models.Asset) []models.Asset {
	if len(releases) == 0 || len(assets) == 0 {
		return nil
	}

	sortedReleases := sortReleasesByVersion(releases)
	info := calculateVersionInfo(sortedReleases)
	keepReleases := p.determineKeepReleases(sortedReleases, info)

	// Find assets to delete
	var toDelete []models.Asset
	for _, a := range assets {
		if !keepReleases[a.ReleaseID] {
			toDelete = append(toDelete, a)
		}
	}

	return toDelete
}

// FilterReleasesToKeep returns releases that should be kept
func (p *Policy) FilterReleasesToKeep(releases []models.Release) []models.Release {
	if len(releases) <= p.MaxVersions {
		return releases
	}

	sortedReleases := sortReleasesByVersion(releases)
	info := calculateVersionInfo(sortedReleases)
	keepReleases := p.determineKeepReleases(sortedReleases, info)

	var result []models.Release
	for _, r := range releases {
		if keepReleases[r.ID] {
			result = append(result, r)
		}
	}

	return result
}
