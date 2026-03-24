package release

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gh-release-monitor/internal/models"
)

// Pre-compiled regex patterns for better performance
var (
	// semverRegex matches semantic versioning: major.minor.patch with optional pre-release
	semverRegex = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[-+].*)?$`)
	// versionPartRegex matches numeric parts for version comparison
	versionPartRegex = regexp.MustCompile(`(\d+)`)
)

// Parser handles release parsing
type Parser struct{}

// NewParser creates a new parser
func NewParser() *Parser {
	return &Parser{}
}

// ParseVersion extracts version components from a tag name
func (p *Parser) ParseVersion(tagName string) (version string, major, minor, patch int) {
	// Remove 'v' prefix
	version = strings.TrimPrefix(tagName, "v")

	// Use pre-compiled regex for semver parsing
	matches := semverRegex.FindStringSubmatch(version)

	if matches == nil {
		return version, 0, 0, 0
	}

	// Regex guarantees these are numeric strings, so Atoi errors are safe to ignore
	major, _ = strconv.Atoi(matches[1])
	if matches[2] != "" {
		minor, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		patch, _ = strconv.Atoi(matches[3])
	}

	return version, major, minor, patch
}

// GetAssetType determines the asset type from filename
func (p *Parser) GetAssetType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	name := strings.ToLower(filename)

	// Check for source archives
	if strings.Contains(name, "source") ||
		strings.Contains(name, "src") ||
		strings.HasSuffix(name, ".source.tar.gz") ||
		strings.HasSuffix(name, ".src.tar.gz") {
		return models.AssetTypeSource
	}

	// Check for checksums
	if ext == ".sha256" || ext == ".sha512" || ext == ".md5" || ext == ".asc" || ext == ".sig" {
		return models.AssetTypeChecksum
	}

	// Installers
	installerExts := map[string]bool{
		".exe": true,
		".msi": true,
		".dmg": true,
		".pkg": true,
		".deb": true,
		".rpm": true,
		".apk": true,
	}
	if installerExts[ext] {
		return models.AssetTypeInstaller
	}

	// Portable/archives
	portableExts := map[string]bool{
		".zip":      true,
		".tar":      true,
		".tar.gz":   true,
		".tgz":      true,
		".tar.bz2":  true,
		".tbz2":     true,
		".tar.xz":   true,
		".txz":      true,
		".7z":       true,
		".appimage": true,
	}

	// Check for compound extensions
	for pe := range portableExts {
		if strings.HasSuffix(name, pe) {
			return models.AssetTypePortable
		}
	}

	return models.AssetTypeOther
}

// ShouldDownloadAsset determines if an asset should be downloaded
func (p *Parser) ShouldDownloadAsset(assetType string) bool {
	// Skip source and checksum files for MVP
	switch assetType {
	case models.AssetTypeInstaller, models.AssetTypePortable:
		return true
	default:
		return false
	}
}

// CompareVersions compares two version strings
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func CompareVersions(v1, v2 string) int {
	// Use pre-compiled regex for version parsing
	v1Parts := versionPartRegex.FindAllString(v1, -1)
	v2Parts := versionPartRegex.FindAllString(v2, -1)

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(v1Parts) {
			n1, _ = strconv.Atoi(v1Parts[i])
		}
		if i < len(v2Parts) {
			n2, _ = strconv.Atoi(v2Parts[i])
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	return 0
}
