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

// DetermineAssetsToDelete determines which assets should be deleted based on retention policy
func (p *Policy) DetermineAssetsToDelete(releases []models.Release, assets []models.Asset) []models.Asset {
	if len(releases) == 0 || len(assets) == 0 {
		return nil
	}

	// Sort releases by version (newest first)
	sortedReleases := make([]models.Release, len(releases))
	copy(sortedReleases, releases)

	sort.Slice(sortedReleases, func(i, j int) bool {
		// Compare by major, minor, patch
		if sortedReleases[i].Major != sortedReleases[j].Major {
			return sortedReleases[i].Major > sortedReleases[j].Major
		}
		if sortedReleases[i].Minor != sortedReleases[j].Minor {
			return sortedReleases[i].Minor > sortedReleases[j].Minor
		}
		return sortedReleases[i].Patch > sortedReleases[j].Patch
	})

	// Find highest minor for each major version
	highestMinorForMajor := make(map[int]int)

	for _, r := range sortedReleases {
		if highest, exists := highestMinorForMajor[r.Major]; !exists || r.Minor > highest {
			highestMinorForMajor[r.Major] = r.Minor
		}
	}

	// Find the highest patch for each major.minor
	highestPatchForMinor := make(map[[2]int]int)
	for _, r := range sortedReleases {
		key := [2]int{r.Major, r.Minor}
		if highest, exists := highestPatchForMinor[key]; !exists || r.Patch > highest {
			highestPatchForMinor[key] = r.Patch
		}
	}

	// Mark releases to keep
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
			if r.Patch == highestPatchForMinor[key] && r.Minor == highestMinorForMajor[r.Major] {
				keepReleases[r.ID] = true
				continue
			}
		}
	}

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

	// Sort releases by version (newest first)
	sortedReleases := make([]models.Release, len(releases))
	copy(sortedReleases, releases)

	sort.Slice(sortedReleases, func(i, j int) bool {
		if sortedReleases[i].Major != sortedReleases[j].Major {
			return sortedReleases[i].Major > sortedReleases[j].Major
		}
		if sortedReleases[i].Minor != sortedReleases[j].Minor {
			return sortedReleases[i].Minor > sortedReleases[j].Minor
		}
		return sortedReleases[i].Patch > sortedReleases[j].Patch
	})

	// Find releases to keep
	keepReleases := make(map[int64]bool)

	// Find highest minor for each major
	highestMinorForMajor := make(map[int]int)
	for _, r := range sortedReleases {
		if highest, exists := highestMinorForMajor[r.Major]; !exists || r.Minor > highest {
			highestMinorForMajor[r.Major] = r.Minor
		}
	}

	// Find highest patch for each major.minor
	highestPatchForMinor := make(map[[2]int]int)
	for _, r := range sortedReleases {
		key := [2]int{r.Major, r.Minor}
		if highest, exists := highestPatchForMinor[key]; !exists || r.Patch > highest {
			highestPatchForMinor[key] = r.Patch
		}
	}

	for i, r := range sortedReleases {
		if i < p.MaxVersions {
			keepReleases[r.ID] = true
			continue
		}

		if p.KeepLastMajor {
			key := [2]int{r.Major, r.Minor}
			if r.Patch == highestPatchForMinor[key] && r.Minor == highestMinorForMajor[r.Major] {
				keepReleases[r.ID] = true
			}
		}
	}

	var result []models.Release
	for _, r := range releases {
		if keepReleases[r.ID] {
			result = append(result, r)
		}
	}

	return result
}
