package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/talyguryn/konta/internal/config"
	"github.com/talyguryn/konta/internal/hooks"
	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/state"
)

// CheckForUpdates checks if a new version is available without updating.
// Used during watch mode to notify user of available updates.
func CheckForUpdates(currentVersion string, updateBehavior string, releaseChannel string) error {
	if updateBehavior == "false" || updateBehavior == "" {
		return nil
	}

	releaseChannel = normalizeReleaseChannel(releaseChannel)
	channelLabel := releaseChannelScopeLabel(releaseChannel)

	release, err := fetchLatestRelease(releaseChannel)
	if err != nil {
		logger.Debug("Failed to check for updates: %v", err)
		return nil
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == currentVersion {
		return nil
	}

	if updateBehavior == "notify" {
		logger.Info("New Konta version available on %s: v%s (current: v%s). Run 'konta update' to install.", channelLabel, latestVersion, currentVersion)
		return nil
	}

	if updateBehavior == "auto" {
		if err := autoUpdate(currentVersion, release); err != nil {
			logger.Warn("Auto-update failed: %v", err)
		}
		return nil
	}

	return nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type githubRateLimit struct {
	Rate struct {
		Limit     int   `json:"limit"`
		Remaining int   `json:"remaining"`
		Reset     int64 `json:"reset"`
	} `json:"rate"`
}

func getGitHubRateLimitReset() (int64, error) {
	resp, err := http.Get("https://api.github.com/rate_limit")
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	var rateLimit githubRateLimit
	if err := json.NewDecoder(resp.Body).Decode(&rateLimit); err != nil {
		return 0, err
	}

	return rateLimit.Rate.Reset, nil
}

func formatRateLimitReset(resetTime int64) string {
	now := time.Now().Unix()
	diff := resetTime - now

	if diff <= 0 {
		return "now"
	}

	minutes := diff / 60
	seconds := diff % 60

	if minutes == 0 {
		return fmt.Sprintf("in %d seconds", seconds)
	}

	if minutes < 60 {
		if seconds == 0 {
			return fmt.Sprintf("in %d minutes", minutes)
		}
		return fmt.Sprintf("in %d minutes %d seconds", minutes, seconds)
	}

	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		return fmt.Sprintf("in %d hours", hours)
	}
	return fmt.Sprintf("in %d hours %d minutes", hours, remainingMinutes)
}

func buildGitHubErrorMessage(statusCode int, body []byte) string {
	var apiError struct {
		Message       string `json:"message"`
		Documentation string `json:"documentation_url"`
	}
	if err := json.Unmarshal(body, &apiError); err == nil && apiError.Message != "" {
		switch statusCode {
		case 403:
			if strings.Contains(apiError.Message, "rate limit") {
				resetTime, err := getGitHubRateLimitReset()
				if err == nil {
					when := formatRateLimitReset(resetTime)
					return fmt.Sprintf("GitHub API rate limit exceeded. You can try again %s.", when)
				}
				return "GitHub API rate limit exceeded. Please try again later."
			}
			return fmt.Sprintf("Access denied by GitHub API. %s", apiError.Message)
		case 404:
			return "Release not found on GitHub"
		default:
			return fmt.Sprintf("GitHub API error - %s", apiError.Message)
		}
	}

	switch statusCode {
	case 403:
		resetTime, err := getGitHubRateLimitReset()
		if err == nil {
			when := formatRateLimitReset(resetTime)
			return fmt.Sprintf("GitHub API rate limit exceeded. You can try again %s.", when)
		}
		return "GitHub API rate limit exceeded. Please try again later."
	case 404:
		return "Release not found on GitHub"
	case 500, 502, 503, 504:
		return "GitHub service temporarily unavailable. Please try again later."
	default:
		return fmt.Sprintf("Error while checking updates: GitHub API returned status %d", statusCode)
	}
}

func fetchLatestAnyRelease() (*githubRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/talyguryn/konta/releases?per_page=30")
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to GitHub - %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read response - %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf(buildGitHubErrorMessage(resp.StatusCode, body))
	}

	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("Failed to parse release info")
	}

	for _, rel := range releases {
		if rel.Draft {
			continue
		}

		release := &githubRelease{TagName: rel.TagName}
		for _, asset := range rel.Assets {
			release.Assets = append(release.Assets, struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				Name:               asset.Name,
				BrowserDownloadURL: asset.BrowserDownloadURL,
			})
		}
		return release, nil
	}

	return nil, fmt.Errorf("No releases found on GitHub")
}

func fetchLatestRelease(channel string) (*githubRelease, error) {
	if normalizeReleaseChannel(channel) == "next" {
		return fetchLatestAnyRelease()
	}

	return fetchLatestStableRelease()
}

func fetchLatestStableRelease() (*githubRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/talyguryn/konta/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to GitHub - %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read response - %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf(buildGitHubErrorMessage(resp.StatusCode, body))
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("Failed to parse release info")
	}

	return &release, nil
}

func normalizeReleaseChannel(channel string) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != "next" {
		return "stable"
	}
	return "next"
}

func releaseChannelScopeLabel(channel string) string {
	if normalizeReleaseChannel(channel) == "next" {
		return "next (latest prerelease or stable)"
	}
	return "stable (latest stable only)"
}

func getBinaryName() string {
	binaryName := fmt.Sprintf("konta-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		binaryName = "konta-linux"
	}
	return binaryName
}

func findDownloadURL(release *githubRelease, binaryName string) string {
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func downloadAndInstall(downloadURL string, latestVersion string) error {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	tmpFile := exePath + ".new"
	out, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	_, err = io.Copy(out, resp.Body)
	if closeErr := out.Close(); closeErr != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to close temp file: %v", closeErr)
	}
	if err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("download failed: %v", err)
	}

	if err := os.Chmod(tmpFile, 0755); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to set permissions: %v", err)
	}

	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to backup current binary: %v", err)
	}

	if err := os.Rename(tmpFile, exePath); err != nil {
		_ = os.Rename(backupPath, exePath)
		return fmt.Errorf("failed to install new binary: %v", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

func runPostUpdateHook() {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return
	}
	defer devNull.Close()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = devNull
	os.Stderr = devNull

	cfg, err := config.Load()
	if err != nil {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		return
	}

	repoDir := state.GetCurrentLink()
	if _, err := os.Stat(repoDir); err != nil {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		return
	}

	hookRunner := hooks.New(repoDir, cfg.Hooks.StartedAbs, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)
	_ = hookRunner.RunPostUpdate()

	os.Stdout = oldStdout
	os.Stderr = oldStderr
}

func autoUpdate(currentVersion string, release *githubRelease) error {
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == currentVersion {
		return nil
	}

	binaryName := getBinaryName()
	downloadURL := findDownloadURL(release, binaryName)
	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	logger.Info("Auto-update: downloading %s (v%s)", binaryName, latestVersion)
	if err := downloadAndInstall(downloadURL, latestVersion); err != nil {
		return err
	}

	runPostUpdateHook()

	isDaemonRunning := isDaemonCurrentlyRunning()

	if isDaemonRunning {
		logger.Info("Auto-update complete: v%s installed. Restarting daemon...", latestVersion)

		if err := restartDaemonForCurrentOS(); err != nil {
			logger.Warn("Failed to restart daemon after auto-update: %v", err)
			logger.Info("Please restart manually: sudo konta restart")
		} else {
			logger.Info("Daemon restarted successfully with new version")
		}
	} else {
		logger.Info("Auto-update complete: v%s installed. Daemon is not running.", latestVersion)
	}

	return nil
}

// Update checks for and installs the latest version from GitHub.
func Update(currentVersion string, forceYes bool, releaseChannelOverride string) error {
	fmt.Printf("Current version: v%s\n", currentVersion)

	releaseChannel := "stable"
	if cfg, err := config.Load(); err == nil {
		releaseChannel = normalizeReleaseChannel(cfg.ReleaseChannel)
	}
	if strings.TrimSpace(releaseChannelOverride) != "" {
		releaseChannel = normalizeReleaseChannel(releaseChannelOverride)
	}
	channelLabel := releaseChannelScopeLabel(releaseChannel)

	fmt.Printf("Checking for updates from GitHub (channel: %s)...\n", channelLabel)
	fmt.Println()

	release, err := fetchLatestRelease(releaseChannel)
	if err != nil {
		return err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	if latestVersion == currentVersion {
		fmt.Printf("✓ Already running the latest version on %s!\n", channelLabel)
		return nil
	}

	fmt.Printf("🎉 New version available on %s: v%s\n", channelLabel, latestVersion)

	if !forceYes {
		fmt.Print("Download and install? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer != "" && answer != "y" && answer != "yes" {
			fmt.Println("Update cancelled")
			return nil
		}
	}

	binaryName := getBinaryName()
	downloadURL := findDownloadURL(release, binaryName)
	if downloadURL == "" {
		return fmt.Errorf("Update failed: no binary found for %s/%s. Try to check updates later again.", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("\nDownloading %s...\n", binaryName)
	if err := downloadAndInstall(downloadURL, latestVersion); err != nil {
		return err
	}

	fmt.Printf("✓ Updated to v%s successfully!\n", latestVersion)

	runPostUpdateHook()

	isDaemonRunning := isDaemonCurrentlyRunning()

	if isDaemonRunning {
		fmt.Println("\nDaemon is running. Attempting automatic restart to apply new version...")
		if os.Getuid() != 0 {
			fmt.Println("\n⚠  Root privileges required to restart daemon.")
			fmt.Println("Restart manually with: sudo konta restart")
			return nil
		}

		if err := restartDaemonForCurrentOS(); err != nil {
			fmt.Printf("⚠  Failed to restart daemon: %v\n", err)
			fmt.Println("Restart manually with: sudo konta restart")
			return nil
		}
		fmt.Println("✓ Daemon restarted with new version!")
	} else {
		fmt.Println("\nDaemon is not running. Start it when ready:")
		fmt.Println("  sudo konta start")
	}

	return nil
}