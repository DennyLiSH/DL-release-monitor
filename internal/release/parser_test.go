package release

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input       string
		wantVersion string
		wantMajor   int
		wantMinor   int
		wantPatch   int
	}{
		{"v1.2.3", "1.2.3", 1, 2, 3},
		{"1.2.3", "1.2.3", 1, 2, 3},
		{"v1.2", "1.2", 1, 2, 0},
		{"v1", "1", 1, 0, 0},
		{"v1.2.3-alpha", "1.2.3-alpha", 1, 2, 3},
		{"v1.2.3-beta.1", "1.2.3-beta.1", 1, 2, 3},
		{"v1.2.3+build.123", "1.2.3+build.123", 1, 2, 3},
		{"invalid", "invalid", 0, 0, 0},
		{"", "", 0, 0, 0},
	}

	parser := NewParser()
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			version, major, minor, patch := parser.ParseVersion(tt.input)
			if version != tt.wantVersion {
				t.Errorf("ParseVersion(%q).version = %q, want %q", tt.input, version, tt.wantVersion)
			}
			if major != tt.wantMajor {
				t.Errorf("ParseVersion(%q).major = %d, want %d", tt.input, major, tt.wantMajor)
			}
			if minor != tt.wantMinor {
				t.Errorf("ParseVersion(%q).minor = %d, want %d", tt.input, minor, tt.wantMinor)
			}
			if patch != tt.wantPatch {
				t.Errorf("ParseVersion(%q).patch = %d, want %d", tt.input, patch, tt.wantPatch)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "1.0.1", -1},
		{"1.2.3", "1.2.4", -1},
		{"1.2", "1.2.0", 0},
		{"2.0.0", "10.0.0", -1},
		{"10.0.0", "2.0.0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.v1+" vs "+tt.v2, func(t *testing.T) {
			got := CompareVersions(tt.v1, tt.v2)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestGetAssetType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"app.exe", "installer"},
		{"app.msi", "installer"},
		{"app.dmg", "installer"},
		{"app.deb", "installer"},
		{"app.rpm", "installer"},
		{"app.zip", "portable"},
		{"app.tar.gz", "portable"},
		{"app.7z", "portable"},
		{"app.AppImage", "portable"},
		{"checksum.sha256", "checksum"},
		{"file.md5", "checksum"},
		{"source.tar.gz", "source"},
		{"src.zip", "source"},
		{"unknown.txt", "other"},
	}

	parser := NewParser()
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := parser.GetAssetType(tt.filename)
			if got != tt.want {
				t.Errorf("GetAssetType(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestShouldDownloadAsset(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		assetType string
		want      bool
	}{
		{"installer", true},
		{"portable", true},
		{"source", false},
		{"checksum", false},
		{"other", false},
	}

	for _, tt := range tests {
		t.Run(tt.assetType, func(t *testing.T) {
			got := parser.ShouldDownloadAsset(tt.assetType)
			if got != tt.want {
				t.Errorf("ShouldDownloadAsset(%q) = %v, want %v", tt.assetType, got, tt.want)
			}
		})
	}
}
