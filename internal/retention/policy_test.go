package retention

import (
	"testing"
	"time"

	"gh-release-monitor/internal/models"
)

func TestNewPolicy(t *testing.T) {
	p := NewPolicy(5, true)
	if p.MaxVersions != 5 {
		t.Errorf("expected MaxVersions 5, got %d", p.MaxVersions)
	}
	if !p.KeepLastMajor {
		t.Error("expected KeepLastMajor to be true")
	}
}

func TestSortReleasesByVersion(t *testing.T) {
	tests := []struct {
		name     string
		releases []models.Release
		expected []int64 // expected IDs in order
	}{
		{
			name:     "empty",
			releases: []models.Release{},
			expected: []int64{},
		},
		{
			name:     "single",
			releases: []models.Release{{ID: 1, Major: 1, Minor: 0, Patch: 0}},
			expected: []int64{1},
		},
		{
			name: "sorted by major",
			releases: []models.Release{
				{ID: 1, Major: 1, Minor: 0, Patch: 0},
				{ID: 2, Major: 2, Minor: 0, Patch: 0},
				{ID: 3, Major: 3, Minor: 0, Patch: 0},
			},
			expected: []int64{3, 2, 1},
		},
		{
			name: "sorted by minor",
			releases: []models.Release{
				{ID: 1, Major: 1, Minor: 0, Patch: 0},
				{ID: 2, Major: 1, Minor: 2, Patch: 0},
				{ID: 3, Major: 1, Minor: 1, Patch: 0},
			},
			expected: []int64{2, 3, 1},
		},
		{
			name: "sorted by patch",
			releases: []models.Release{
				{ID: 1, Major: 1, Minor: 0, Patch: 1},
				{ID: 2, Major: 1, Minor: 0, Patch: 3},
				{ID: 3, Major: 1, Minor: 0, Patch: 2},
			},
			expected: []int64{2, 3, 1},
		},
		{
			name: "complex sorting",
			releases: []models.Release{
				{ID: 1, Major: 1, Minor: 0, Patch: 0},
				{ID: 2, Major: 2, Minor: 0, Patch: 0},
				{ID: 3, Major: 1, Minor: 2, Patch: 0},
				{ID: 4, Major: 1, Minor: 1, Patch: 5},
				{ID: 5, Major: 1, Minor: 1, Patch: 3},
			},
			expected: []int64{2, 3, 4, 5, 1}, // 2.0.0, 1.2.0, 1.1.5, 1.1.3, 1.0.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorted := sortReleasesByVersion(tt.releases)
			if len(sorted) != len(tt.expected) {
				t.Errorf("expected %d releases, got %d", len(tt.expected), len(sorted))
				return
			}
			for i, r := range sorted {
				if r.ID != tt.expected[i] {
					t.Errorf("position %d: expected ID %d, got %d", i, tt.expected[i], r.ID)
				}
			}
		})
	}
}

func TestCalculateVersionInfo(t *testing.T) {
	releases := []models.Release{
		{Major: 1, Minor: 0, Patch: 0},
		{Major: 1, Minor: 1, Patch: 0},
		{Major: 1, Minor: 1, Patch: 5},
		{Major: 2, Minor: 0, Patch: 0},
		{Major: 2, Minor: 1, Patch: 3},
	}

	info := calculateVersionInfo(releases)

	tests := []struct {
		name     string
		key      any
		expected int
	}{
		{"major 1 highest minor", [2]int{1, -1}, 1}, // highestMinorForMajor[1]
		{"major 2 highest minor", [2]int{2, -1}, 1}, // highestMinorForMajor[2]
		{"1.0 highest patch", [2]int{1, 0}, 0},
		{"1.1 highest patch", [2]int{1, 1}, 5},
		{"2.1 highest patch", [2]int{2, 1}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			if key, ok := tt.key.([2]int); ok {
				if key[1] == -1 {
					// This is a major version lookup
					got = info.highestMinorForMajor[key[0]]
				} else {
					got = info.highestPatchForMinor[key]
				}
			}
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestDetermineKeepReleases(t *testing.T) {
	t.Run("basic retention without keepLastMajor", func(t *testing.T) {
		p := NewPolicy(3, false)

		releases := []models.Release{
			{ID: 1, Major: 3, Minor: 0, Patch: 0},
			{ID: 2, Major: 2, Minor: 0, Patch: 0},
			{ID: 3, Major: 1, Minor: 5, Patch: 0},
			{ID: 4, Major: 1, Minor: 0, Patch: 0},
			{ID: 5, Major: 0, Minor: 1, Patch: 0},
		}

		sorted := sortReleasesByVersion(releases)
		info := calculateVersionInfo(sorted)
		keep := p.determineKeepReleases(sorted, info)

		// Should keep first 3 (newest): 3.0.0, 2.0.0, 1.5.0
		if !keep[1] {
			t.Error("3.0.0 should be kept")
		}
		if !keep[2] {
			t.Error("2.0.0 should be kept")
		}
		if !keep[3] {
			t.Error("1.5.0 should be kept")
		}
		if keep[4] {
			t.Error("1.0.0 should be deleted")
		}
		if keep[5] {
			t.Error("0.1.0 should be deleted")
		}
	})

	t.Run("retention with keepLastMajor", func(t *testing.T) {
		p := NewPolicy(2, true)

		releases := []models.Release{
			{ID: 1, Major: 2, Minor: 1, Patch: 0}, // Newest
			{ID: 2, Major: 2, Minor: 0, Patch: 0}, // Second newest
			{ID: 3, Major: 1, Minor: 2, Patch: 0}, // Last of major 1
			{ID: 4, Major: 1, Minor: 1, Patch: 0},
			{ID: 5, Major: 1, Minor: 0, Patch: 0},
			{ID: 6, Major: 0, Minor: 1, Patch: 0}, // Last of major 0
		}

		sorted := sortReleasesByVersion(releases)
		info := calculateVersionInfo(sorted)
		keep := p.determineKeepReleases(sorted, info)

		if !keep[1] {
			t.Error("2.1.0 should be kept (within max)")
		}
		if !keep[2] {
			t.Error("2.0.0 should be kept (within max)")
		}
		if !keep[3] {
			t.Error("1.2.0 should be kept (last of major 1)")
		}
		if keep[4] {
			t.Error("1.1.0 should be deleted")
		}
		if keep[5] {
			t.Error("1.0.0 should be deleted")
		}
		if !keep[6] {
			t.Error("0.1.0 should be kept (last of major 0)")
		}
	})
}

func TestDetermineAssetsToDelete(t *testing.T) {
	t.Run("empty releases", func(t *testing.T) {
		p := NewPolicy(5, false)
		assets := []models.Asset{{ID: 1, ReleaseID: 1}}
		result := p.DetermineAssetsToDelete([]models.Release{}, assets)
		if result != nil {
			t.Errorf("expected nil for empty releases, got %v", result)
		}
	})

	t.Run("empty assets", func(t *testing.T) {
		p := NewPolicy(5, false)
		releases := []models.Release{{ID: 1}}
		result := p.DetermineAssetsToDelete(releases, []models.Asset{})
		if result != nil {
			t.Errorf("expected nil for empty assets, got %v", result)
		}
	})

	t.Run("basic deletion", func(t *testing.T) {
		p := NewPolicy(2, false)

		releases := []models.Release{
			{ID: 10, Major: 2, Minor: 0, Patch: 0},
			{ID: 20, Major: 1, Minor: 0, Patch: 0},
			{ID: 30, Major: 0, Minor: 1, Patch: 0},
		}

		assets := []models.Asset{
			{ID: 1, ReleaseID: 10}, // Keep
			{ID: 2, ReleaseID: 20}, // Keep
			{ID: 3, ReleaseID: 30}, // Delete
			{ID: 4, ReleaseID: 30}, // Delete
		}

		toDelete := p.DetermineAssetsToDelete(releases, assets)

		if len(toDelete) != 2 {
			t.Errorf("expected 2 assets to delete, got %d", len(toDelete))
			return
		}

		deletedIDs := make(map[int64]bool)
		for _, a := range toDelete {
			deletedIDs[a.ID] = true
		}
		if !deletedIDs[3] {
			t.Error("asset 3 should be deleted")
		}
		if !deletedIDs[4] {
			t.Error("asset 4 should be deleted")
		}
	})
}

func TestFilterReleasesToKeep(t *testing.T) {
	t.Run("within max", func(t *testing.T) {
		p := NewPolicy(5, false)
		releases := []models.Release{
			{ID: 1, Major: 1, Minor: 0, Patch: 0},
			{ID: 2, Major: 0, Minor: 1, Patch: 0},
		}

		result := p.FilterReleasesToKeep(releases)
		if len(result) != 2 {
			t.Errorf("expected 2 releases, got %d", len(result))
		}
	})

	t.Run("exceeds max", func(t *testing.T) {
		p := NewPolicy(2, false)
		releases := []models.Release{
			{ID: 1, Major: 2, Minor: 0, Patch: 0},
			{ID: 2, Major: 1, Minor: 1, Patch: 0},
			{ID: 3, Major: 1, Minor: 0, Patch: 0},
			{ID: 4, Major: 0, Minor: 1, Patch: 0},
		}

		result := p.FilterReleasesToKeep(releases)
		if len(result) != 2 {
			t.Errorf("expected 2 releases, got %d", len(result))
			return
		}

		keptIDs := make(map[int64]bool)
		for _, r := range result {
			keptIDs[r.ID] = true
		}
		if !keptIDs[1] {
			t.Error("2.0.0 should be kept")
		}
		if !keptIDs[2] {
			t.Error("1.1.0 should be kept")
		}
	})

	t.Run("keep last major", func(t *testing.T) {
		p := NewPolicy(1, true)
		releases := []models.Release{
			{ID: 1, Major: 2, Minor: 0, Patch: 0},
			{ID: 2, Major: 1, Minor: 1, Patch: 0},
			{ID: 3, Major: 1, Minor: 0, Patch: 0},
			{ID: 4, Major: 0, Minor: 1, Patch: 0},
		}

		result := p.FilterReleasesToKeep(releases)

		keptIDs := make(map[int64]bool)
		for _, r := range result {
			keptIDs[r.ID] = true
		}

		if !keptIDs[1] {
			t.Error("2.0.0 should be kept (within max)")
		}
		if !keptIDs[2] {
			t.Error("1.1.0 should be kept (last of major 1)")
		}
		if keptIDs[3] {
			t.Error("1.0.0 should be deleted")
		}
		if !keptIDs[4] {
			t.Error("0.1.0 should be kept (last of major 0)")
		}
	})
}

func TestDetermineAssetsToDelete_RealWorld(t *testing.T) {
	p := NewPolicy(3, true)

	now := time.Now()
	releases := []models.Release{
		{ID: 1, Major: 3, Minor: 0, Patch: 0, PublishedAt: now.Add(-1 * time.Hour)},
		{ID: 2, Major: 2, Minor: 1, Patch: 0, PublishedAt: now.Add(-2 * time.Hour)},
		{ID: 3, Major: 2, Minor: 0, Patch: 0, PublishedAt: now.Add(-3 * time.Hour)},
		{ID: 4, Major: 1, Minor: 5, Patch: 0, PublishedAt: now.Add(-4 * time.Hour)},
		{ID: 5, Major: 1, Minor: 4, Patch: 0, PublishedAt: now.Add(-5 * time.Hour)},
		{ID: 6, Major: 1, Minor: 0, Patch: 0, PublishedAt: now.Add(-10 * time.Hour)},
	}

	assets := []models.Asset{
		{ID: 101, ReleaseID: 1, Name: "v3.0.0.tar.gz"},
		{ID: 102, ReleaseID: 2, Name: "v2.1.0.tar.gz"},
		{ID: 103, ReleaseID: 3, Name: "v2.0.0.tar.gz"},
		{ID: 104, ReleaseID: 4, Name: "v1.5.0.tar.gz"},
		{ID: 105, ReleaseID: 5, Name: "v1.4.0.tar.gz"},
		{ID: 106, ReleaseID: 6, Name: "v1.0.0.tar.gz"},
	}

	toDelete := p.DetermineAssetsToDelete(releases, assets)

	deletedIDs := make(map[int64]bool)
	for _, a := range toDelete {
		deletedIDs[a.ID] = true
	}

	// Keep: 3.0.0, 2.1.0, 2.0.0 (within max 3)
	// Also keep: 1.5.0 (last of major 1)
	// Delete: 1.4.0, 1.0.0
	if deletedIDs[101] {
		t.Error("v3.0.0 should be kept")
	}
	if deletedIDs[102] {
		t.Error("v2.1.0 should be kept")
	}
	if deletedIDs[103] {
		t.Error("v2.0.0 should be kept")
	}
	if deletedIDs[104] {
		t.Error("v1.5.0 should be kept (last of major 1)")
	}
	if !deletedIDs[105] {
		t.Error("v1.4.0 should be deleted")
	}
	if !deletedIDs[106] {
		t.Error("v1.0.0 should be deleted")
	}
}
