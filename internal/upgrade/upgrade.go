package upgrade

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const releaseURL = "https://api.github.com/repos/VladGavrila/proxmox-cli/releases/latest"

// githubRelease is a minimal representation of a GitHub release.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset is a minimal representation of a GitHub release asset.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Run checks for the latest release and upgrades the current binary in place.
func Run(currentVersion string) error {
	// 1. Detect current binary path (resolve symlinks).
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	binaryPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// 2. Fetch latest release from GitHub.
	fmt.Fprintln(os.Stderr, "Checking for updates...")
	rel, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")

	// 3. Compare versions.
	cmp := compareVersions(latestVersion, currentVersion)
	if cmp <= 0 {
		fmt.Printf("Already up to date (v%s)\n", currentVersion)
		return nil
	}

	fmt.Printf("Upgrading from %s to %s...\n", currentVersion, latestVersion)

	// 4. Find matching asset for this OS/arch.
	wantName, err := assetName()
	if err != nil {
		return err
	}

	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == wantName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no release asset found for %s (expected %s)", runtime.GOOS+"/"+runtime.GOARCH, wantName)
	}

	// 5. Download binary to a temp file in the same directory (same filesystem for atomic rename).
	dir := filepath.Dir(binaryPath)
	tmpFile, err := os.CreateTemp(dir, ".pxve-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // clean up on any failure path

	fmt.Fprintln(os.Stderr, "Downloading...")
	resp, err := http.Get(downloadURL) //nolint:gosec
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write update: %w", err)
	}
	tmpFile.Close()

	// 6. Set executable permissions and atomically replace.
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if err := os.Rename(tmpPath, binaryPath); err != nil {
		return fmt.Errorf("failed to replace binary (try: sudo pxve --upgrade): %w", err)
	}

	// 7. Success.
	fmt.Printf("Updated pxve to v%s\n", latestVersion)
	return nil
}

// fetchLatestRelease fetches the latest release metadata from GitHub.
func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", releaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}
	return &rel, nil
}

// compareVersions compares two semver strings (without "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

// parseSemver splits a version string into [major, minor, patch].
// Missing or non-numeric segments default to 0.
func parseSemver(v string) [3]int {
	var parts [3]int
	segs := strings.SplitN(v, ".", 3)
	for i, s := range segs {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(s)
		parts[i] = n
	}
	return parts
}

// assetName returns the expected release asset filename for the current platform.
func assetName() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "pxve-macos-arm64", nil
	case "linux/amd64":
		return "pxve-linux-amd64", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}
