package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/talyguryn/konta/internal/config"
	"github.com/talyguryn/konta/internal/git"
	"github.com/talyguryn/konta/internal/hooks"
	"github.com/talyguryn/konta/internal/lock"
	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/reconcile"
	"github.com/talyguryn/konta/internal/state"
	"github.com/talyguryn/konta/internal/types"
)

// PrintUsage prints usage information
func PrintUsage(version string) {
	fmt.Printf(`Konta v%s
https://github.com/talyguryn/konta

GitOps for Docker Compose

Usage:
	konta install [OPTIONS]
	konta uninstall
	konta run [--dry-run] [--watch]
	konta daemon [enable|disable|restart|status]
	konta enable | konta disable | konta restart | konta status
	konta journal
	konta config [-e]
	konta update [-y]
	konta version (-v)
	konta help (-h)

Install Options:
  --repo URL                        GitHub repository URL (required)
  --path PATH                       Base path in repo (contains 'apps' dir, default: repo root)
  --branch BRANCH                   Git branch (default: main)
  --interval SECONDS                Polling interval (default: 120)
  --token TOKEN                     GitHub token (or set KONTA_TOKEN env)

Short flags:
  -h, --help                        Show this help
  -v, --version                     Show version
  -r                                Same as 'run'
  -d                                Same as 'daemon enable' (or 'start')
  -s                                Show daemon status
  -j                                Show live logs (same as 'journal')

Update flags:
  -y                                Skip confirmation and auto-update

Examples:
  konta install                     # Interactive setup
  konta install --repo https://github.com/user/infra
  konta install --repo https://github.com/talyguryn/konta --path spb
  konta run                         # Single reconciliation
  konta run --watch                 # Watch mode (poll every N seconds)
  konta run --dry-run               # Show what would change
  konta start                       # Start the daemon
  konta stop                        # Stop the daemon
  konta restart                     # Restart the daemon
  konta status                      # Check daemon status
  konta journal                     # View live logs
  konta journal -f                  # Same as 'konta journal'
  konta update                      # Update to latest version (interactive)
  konta update -y                   # Update without confirmation

Environment:
  KONTA_TOKEN                       GitHub token (alternative to --token)

More info: https://github.com/talyguryn/konta
`, version)
}

// Config prints the contents of the active config file or opens it in an editor.
func Config(edit bool) error {
	configPath, err := config.FindConfigPath()
	if err != nil {
		return err
	}

	if edit {
		cmd := exec.Command("nano", configPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	fmt.Printf("%s\n", configPath)
	_, err = os.Stdout.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write config output: %w", err)
	}

	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Println()
	}

	return nil
}

// Install performs first-time setup with optional CLI parameters
// Usage: konta install [--repo URL] [--path PATH] [--branch BRANCH] [--interval SECONDS] [--token TOKEN] [--konta_updates auto|notify|false]
func Install(args []string) error {
	logger.Info("Starting Konta installation")

	// Parse command-line arguments
	var (
		repoURL      string
		branch       string
		appsPath     string
		interval     int
		token        string
		kontaUpdates string
	)

	// Parse flags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 < len(args) {
				repoURL = args[i+1]
				i++
			}
		case "--path":
			if i+1 < len(args) {
				appsPath = args[i+1]
				i++
			}
		case "--branch":
			if i+1 < len(args) {
				branch = args[i+1]
				i++
			}
		case "--interval":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					interval = n
				}
				i++
			}
		case "--token":
			if i+1 < len(args) {
				token = args[i+1]
				i++
			}
		case "--konta_updates":
			if i+1 < len(args) {
				kontaUpdates = args[i+1]
				i++
			}
		}
	}

	// If no args provided, use interactive mode
	if repoURL == "" {
		return installInteractive()
	}

	// Set defaults for missing values
	if branch == "" {
		branch = "main"
	}
	if appsPath == "" {
		appsPath = "."
	}
	if interval == 0 {
		interval = 120
	}
	if kontaUpdates == "" {
		kontaUpdates = "notify"
	}

	// Get token from environment if not provided via CLI
	if token == "" {
		token = os.Getenv("KONTA_TOKEN")
	}

	// Validate inputs
	logger.Info("Validating configuration parameters...")
	if err := validateInstallParams(repoURL, branch, appsPath, interval); err != nil {
		return err
	}

	// Test repository connection
	logger.Info("Testing repository connection to: %s", repoURL)
	if err := testRepositoryConnection(repoURL, branch, token); err != nil {
		return fmt.Errorf("repository connection failed: %w", err)
	}
	logger.Info("✓ Repository connection successful")

	// Create configuration
	cfg := &types.Config{
		Version: "v1",
		Repository: types.RepositoryConf{
			URL:      repoURL,
			Branch:   branch,
			Token:    token,
			Path:     appsPath,
			Interval: interval,
		},
		Deploy: types.DeployConf{
			Atomic: true,
		},
		Logging: types.LoggingConf{
			Level: "info",
		},
		KontaUpdates: kontaUpdates,
	}

	// Initialize directories
	logger.Info("Initializing Konta state directory...")
	if err := state.Init(); err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}
	logger.Info("✓ State directory initialized")

	// Save configuration
	logger.Info("Saving configuration...")
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	logger.Info("✓ Configuration saved to /etc/konta/config.yaml")

	// Display summary
	fmt.Println()
	fmt.Println("✅ Setup complete!")
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Printf("  Repository: %s\n", repoURL)
	fmt.Printf("  Branch:     %s\n", branch)
	fmt.Printf("  Base path:  %s\n", appsPath)
	fmt.Printf("  Interval:   %d seconds\n", interval)
	fmt.Printf("  Auto-update: %s\n", kontaUpdates)

	fmt.Println()
	fmt.Println("Starting daemon...")
	if err := daemonEnable("konta", "/etc/systemd/system/konta.service"); err != nil {
		logger.Warn("Failed to auto-enable daemon: %v", err)
		fmt.Printf("\n⚠️  Could not auto-start daemon. To enable it manually, run:\n")
		fmt.Printf("    sudo konta daemon enable\n")
	}

	return nil
}

// installInteractive performs installation in interactive mode
func installInteractive() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Repository URL (e.g., https://github.com/user/infra): ")
	repoURL, _ := reader.ReadString('\n')
	repoURL = strings.TrimSpace(repoURL)

	if repoURL == "" {
		return fmt.Errorf("repository URL is required")
	}

	fmt.Print("Branch [main]: ")
	branch, _ := reader.ReadString('\n')
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}

	fmt.Print("Base path in repo containing 'apps' folder [repo root]: ")
	appsPath, _ := reader.ReadString('\n')
	appsPath = strings.TrimSpace(appsPath)
	if appsPath == "" {
		appsPath = "."
	}

	fmt.Print("Polling interval in seconds [120]: ")
	intervalStr, _ := reader.ReadString('\n')
	intervalStr = strings.TrimSpace(intervalStr)
	interval := 120
	if i, err := strconv.Atoi(intervalStr); err == nil && i > 0 {
		interval = i
	}

	fmt.Print("GitHub token (optional, or set KONTA_TOKEN env): ")
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)

	fmt.Print("Check for Konta updates [auto/notify/false] [notify]: ")
	kontaUpdates, _ := reader.ReadString('\n')
	kontaUpdates = strings.TrimSpace(kontaUpdates)
	if kontaUpdates == "" {
		kontaUpdates = "notify"
	}
	// Validate update setting
	if kontaUpdates != "auto" && kontaUpdates != "notify" && kontaUpdates != "false" {
		kontaUpdates = "notify"
		fmt.Printf("Invalid setting, using default: %s\n", kontaUpdates)
	}

	// Create configuration
	cfg := &types.Config{
		Version: "v1",
		Repository: types.RepositoryConf{
			URL:      repoURL,
			Branch:   branch,
			Token:    token,
			Path:     appsPath,
			Interval: interval,
		},
		Deploy: types.DeployConf{
			Atomic: true,
		},
		Logging: types.LoggingConf{
			Level: "info",
		},
		KontaUpdates: kontaUpdates,
	}

	// Initialize directories
	if err := state.Init(); err != nil {
		return err
	}

	// Save configuration
	if err := config.Save(cfg); err != nil {
		return err
	}

	logger.Info("Installation complete")
	fmt.Println("\n✅ Setup complete!")
	fmt.Println("\nStarting daemon...")

	// Automatically enable and start daemon
	if err := daemonEnable("konta", "/etc/systemd/system/konta.service"); err != nil {
		logger.Warn("Failed to auto-enable daemon: %v", err)
		fmt.Printf("\n⚠️  Could not auto-start daemon. To enable it manually, run:\n")
		fmt.Printf("    sudo konta daemon enable\n")
	}

	return nil
}

// validateInstallParams validates installation parameters
func validateInstallParams(repoURL, branch, appsPath string, interval int) error {
	if repoURL == "" {
		return fmt.Errorf("repository URL is required")
	}
	if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") {
		return fmt.Errorf("repository URL must start with http:// or https://")
	}
	if branch == "" {
		return fmt.Errorf("branch is required")
	}
	if appsPath == "" {
		return fmt.Errorf("base path is required")
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	logger.Info("✓ All parameters valid")
	return nil
}

// testRepositoryConnection tests if we can connect to the repository
func testRepositoryConnection(repoURL, branch, token string) error {
	logger.Info("Testing connection with git...")

	tempDir, err := os.MkdirTemp("", "konta-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	logger.Debug("Using temporary directory: %s", tempDir)

	// Try to clone the repo with depth 1 just to test connection
	cfgCopy := &types.RepositoryConf{
		URL:    repoURL,
		Branch: branch,
		Token:  token,
	}

	_, err = git.Clone(cfgCopy, tempDir)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	logger.Info("✓ Successfully cloned test repository")
	return nil
}

// Uninstall removes all Konta files and directories
func Uninstall() error {
	fmt.Println("This will:")
	fmt.Println("  - Stop and disable Konta daemon (if running)")
	fmt.Println("  - Stop and remove all Konta-managed containers")
	fmt.Println("  - Remove all configuration and state files")
	fmt.Println("  - Keep the binary at /usr/local/bin/konta")
	fmt.Println()
	fmt.Print("Are you sure? (yes/no): ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "yes" && answer != "y" {
		fmt.Println("Uninstall cancelled")
		return nil
	}

	fmt.Println()

	// 1. Stop and disable systemd service
	fmt.Println("1. Stopping Konta daemon...")
	_ = exec.Command("systemctl", "stop", "konta").Run()
	_ = exec.Command("systemctl", "disable", "konta").Run()

	servicePath := "/etc/systemd/system/konta.service"
	if _, err := os.Stat(servicePath); err == nil {
		if err := os.Remove(servicePath); err != nil {
			logger.Warn("Failed to remove systemd service: %v", err)
		} else {
			fmt.Printf("   ✓ Removed systemd service\n")
		}
		_ = exec.Command("systemctl", "daemon-reload").Run()
	} else {
		fmt.Println("   (daemon not installed)")
	}
	fmt.Println()

	// 2. Stop and remove all Konta-managed containers
	fmt.Println("2. Stopping Konta-managed containers...")
	cmd := exec.Command("docker", "ps", "-aq", "--filter", "label=konta.managed=true")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		containerIDs := strings.Fields(string(output))
		if len(containerIDs) > 0 {
			fmt.Printf("   Found %d containers\n", len(containerIDs))
			stopCmd := exec.Command("docker", "stop")
			stopCmd.Args = append(stopCmd.Args, containerIDs...)
			_ = stopCmd.Run()

			rmCmd := exec.Command("docker", "rm")
			rmCmd.Args = append(rmCmd.Args, containerIDs...)
			if err := rmCmd.Run(); err != nil {
				logger.Warn("Failed to remove containers: %v", err)
			} else {
				fmt.Printf("   ✓ All containers stopped and removed\n")
			}
		} else {
			fmt.Println("   (no Konta containers found)")
		}
	} else {
		fmt.Println("   (no Konta containers found)")
	}
	fmt.Println()

	// 3. Remove directories and state
	fmt.Println("3. Removing Konta data and configs...")
	dirs := []string{
		"/etc/konta",
		"/var/lib/konta",
		"/var/log/konta",
		"/var/run/konta.lock",
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		dirs = append(dirs, filepath.Join(homeDir, ".konta"))
	}

	removed := 0
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			if err := os.RemoveAll(dir); err != nil {
				logger.Warn("Failed to remove %s: %v", dir, err)
			} else {
				fmt.Printf("   ✓ Removed: %s\n", dir)
				removed++
			}
		}
	}

	if removed == 0 {
		fmt.Println("   (no Konta data found)")
	}
	fmt.Println()

	fmt.Println("=== UNINSTALL COMPLETE ===")
	fmt.Println()
	fmt.Println("State:")
	fmt.Println("  - Daemon: removed")
	fmt.Println("  - Containers: removed")
	fmt.Println("  - Configs: removed")
	fmt.Println("  - State: removed")
	fmt.Println("  - Binary: preserved at /usr/local/bin/konta")
	fmt.Println()
	fmt.Println("To remove the binary: sudo rm /usr/local/bin/konta")

	return nil
}

// Journal shows live logs from systemd
func Journal() error {
	cmd := exec.Command("journalctl", "-u", "konta", "-f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Println("Showing live logs (Ctrl+C to exit)...")
	fmt.Println()

	return cmd.Run()
}

// Update checks for and installs the latest version from GitHub
// CheckForUpdates checks if a new version is available without updating
// Used during watch mode to notify user of available updates
func CheckForUpdates(currentVersion string, updateBehavior string) error {
	// Skip if updates are disabled
	if updateBehavior == "false" || updateBehavior == "" {
		return nil
	}

	release, err := fetchLatestRelease()
	if err != nil {
		logger.Debug("Failed to check for updates: %v", err)
		return nil // Don't fail on update check errors
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == currentVersion {
		return nil // Already on latest
	}

	if updateBehavior == "notify" {
		logger.Info("New Konta version available: v%s (current: v%s). Run 'konta update' to install.", latestVersion, currentVersion)
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
		Limit     int `json:"limit"`
		Remaining int `json:"remaining"`
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
	// Parse GitHub API error response if available
	var apiError struct {
		Message string `json:"message"`
		Documentation string `json:"documentation_url"`
	}
	if err := json.Unmarshal(body, &apiError); err == nil && apiError.Message != "" {
		switch statusCode {
		case 403:
			// Rate limiting is the most common 403 error
			if strings.Contains(apiError.Message, "rate limit") {
				resetTime, err := getGitHubRateLimitReset()
				if err == nil {
					when := formatRateLimitReset(resetTime)
					return fmt.Sprintf("Error while checking updates: GitHub API rate limit exceeded. You can try again %s.", when)
				}
				return "Error while checking updates: GitHub API rate limit exceeded. Please try again later."
			}
			return fmt.Sprintf("Error while checking updates: Access denied by GitHub API. %s", apiError.Message)
		case 404:
			return "Error while checking updates: Release not found on GitHub"
		default:
			return fmt.Sprintf("Error while checking updates: GitHub API error - %s", apiError.Message)
		}
	}

	// Fallback messages based on status code
	switch statusCode {
	case 403:
		resetTime, err := getGitHubRateLimitReset()
		if err == nil {
			when := formatRateLimitReset(resetTime)
			return fmt.Sprintf("Error while checking updates: GitHub API rate limit exceeded. You can try again %s.", when)
		}
		return "Error while checking updates: GitHub API rate limit exceeded. Please try again later."
	case 404:
		return "Error while checking updates: Release not found on GitHub"
	case 500, 502, 503, 504:
		return "Error while checking updates: GitHub service temporarily unavailable. Please try again later."
	default:
		return fmt.Sprintf("Error while checking updates: GitHub API returned status %d", statusCode)
	}
}

func fetchLatestRelease() (*githubRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/talyguryn/konta/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("error while checking updates: failed to connect to GitHub - %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error while checking updates: failed to read response - %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf(buildGitHubErrorMessage(resp.StatusCode, body))
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("error while checking updates: failed to parse release info")
	}

	return &release, nil
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
	// Suppress all output (logs and hook output) during post-update
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

	hookRunner := hooks.New(repoDir, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)
	_ = hookRunner.RunPostUpdate()

	// Restore stdout and stderr
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

	logger.Info("Auto-update complete: v%s installed. Restart the daemon to apply.", latestVersion)
	return nil
}

func Update(currentVersion string, forceYes bool) error {
	fmt.Printf("Current version: v%s\n", currentVersion)
	fmt.Println("Checking for updates from GitHub...")

	release, err := fetchLatestRelease()
	if err != nil {
		return err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	if latestVersion == currentVersion {
		fmt.Println("✅ Already running the latest version!")
		return nil
	}

	fmt.Printf("\n🎉 New version available: v%s\n", latestVersion)

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
		return fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("\nDownloading %s...\n", binaryName)
	if err := downloadAndInstall(downloadURL, latestVersion); err != nil {
		return err
	}

	fmt.Printf("✅ Updated to v%s successfully!\n", latestVersion)

	runPostUpdateHook()

	// Check if daemon is running and restart it
	statusCmd := exec.Command("systemctl", "is-active", "konta")
	err = statusCmd.Run()
	isDaemonRunning := err == nil

	if isDaemonRunning {
		fmt.Println("\nDaemon is running. Attempting automatic restart to apply new version...")
		if os.Getuid() != 0 {
			fmt.Println("\n⚠️  Root privileges required to restart daemon.")
			fmt.Println("Restart manually with: sudo konta restart")
			return nil
		}

		// Restart the daemon
		restartCmd := exec.Command("systemctl", "restart", "konta")
		if err := restartCmd.Run(); err != nil {
			fmt.Printf("⚠️  Failed to restart daemon: %v\n", err)
			fmt.Println("Restart manually with: sudo konta restart")
			return nil
		}
		fmt.Println("✅ Daemon restarted with new version!")
	} else {
		fmt.Println("\nDaemon is not running. Start it when ready:")
		fmt.Println("  sudo konta start")
	}

	return nil
}

// Run executes reconciliation once or in watch mode
func Run(dryRun bool, watch bool, version string) error {
	// Execute reconciliation once
	if err := reconcileOnce(dryRun, version); err != nil && !watch {
		// Only return error if not in watch mode
		// In watch mode, we log error and continue
		return err
	}

	// If watch mode, enter polling loop
	if watch {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		logger.Info("Watch mode enabled. Polling every %d seconds (Ctrl+C to stop)", cfg.Repository.Interval)

		// Check for updates on first run
		if cfg.KontaUpdates != "" && cfg.KontaUpdates != "false" {
			_ = CheckForUpdates(version, cfg.KontaUpdates)
		}

		// First reconciliation already done above, now enter polling loop
		var ticker *time.Ticker
		ticker = time.NewTicker(time.Duration(cfg.Repository.Interval) * time.Second)
		defer ticker.Stop()

		checkCounter := 0
		checkInterval := 10 // Check for updates every 10 cycles

		// Infinite loop - exit only on signal (Ctrl+C) or systemd stop
		for range ticker.C {
			// Reload config on each iteration to pick up interval changes
			newCfg, err := config.Load()
			if err != nil {
				logger.Error("Failed to reload config: %v", err)
				// Continue with previous config
			} else if newCfg.Repository.Interval != cfg.Repository.Interval {
				// Interval changed, reset ticker
				logger.Info("Config updated: polling interval changed from %d to %d seconds",
					cfg.Repository.Interval, newCfg.Repository.Interval)
				ticker.Stop()
				ticker = time.NewTicker(time.Duration(newCfg.Repository.Interval) * time.Second)
				cfg = newCfg
			} else {
				cfg = newCfg
			}

			// Check for updates periodically (every 10 cycles)
			checkCounter++
			if checkCounter >= checkInterval && cfg.KontaUpdates != "" && cfg.KontaUpdates != "false" {
				checkCounter = 0
				_ = CheckForUpdates(version, cfg.KontaUpdates)
			}

			if err := reconcileOnce(false, version); err != nil {
				logger.Error("Deployment error: %v", err)
				// Continue on error, don't exit
			}
		}
	}

	return nil
}

// reconcileOnce performs a single reconciliation cycle
func reconcileOnce(dryRun bool, version string) error {
	l, err := lock.Acquire()
	if err != nil {
		return err
	}
	defer func() { _ = l.Release() }()

	logger.Info("Konta v%s", version)
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := state.Init(); err != nil {
		return err
	}

	// Get current state
	currentState, err := state.Load()
	if err != nil {
		logger.Warn("Failed to load state: %v", err)
		currentState = &types.State{}
	}

	// Clone/update the repository
	releaseDir := filepath.Join(state.GetReleasesDir(), "temp-"+time.Now().Format("20060102150405"))
	defer func() { _ = os.RemoveAll(releaseDir) }()

	newCommit, err := git.Clone(&cfg.Repository, releaseDir)
	if err != nil {
		return err
	}

	// Check if there are changes
	if newCommit == currentState.LastCommit {
		lastCommitStr := currentState.LastCommit
		if len(lastCommitStr) > 8 {
			lastCommitStr = lastCommitStr[:8]
		}
		logger.Info("No changes detected (current: %s)", lastCommitStr)

		// Even without changes, perform health check to ensure containers are running
		logger.Info("Performing container health check")
		if !dryRun {
			reconciler := reconcile.New(cfg, releaseDir, dryRun)
			reconciler.SetChangedProjects(nil) // nil means check all projects
			if _, err := reconciler.HealthCheck(); err != nil {
				logger.Warn("Health check encountered issues: %v", err)
				// Don't return error, just warn
			}
		}
		return nil
	}

	lastCommitStr := currentState.LastCommit
	if len(lastCommitStr) > 8 {
		lastCommitStr = lastCommitStr[:8]
	} else if lastCommitStr == "" {
		lastCommitStr = "none"
	}
	logger.Info("New commit detected: %s -> %s", lastCommitStr, newCommit[:8])

	// Validate compose path
	if err := git.ValidateComposePath(releaseDir, cfg.Repository.Path); err != nil {
		return err
	}

	// Detect which projects have changed
	changedProjects, err := git.GetChangedProjects(releaseDir, cfg.Repository.Path, currentState.LastCommit, newCommit)
	if err != nil {
		logger.Warn("Failed to detect changed projects: %v (will reconcile all)", err)
		changedProjects = nil // nil means reconcile all
	}

	if changedProjects != nil && len(changedProjects) == 0 {
		logger.Info("No project changes detected in %s, skipping reconciliation", cfg.Repository.Path)
		if !dryRun {
			if err := state.UpdateWithProjects(newCommit, []string{}); err != nil {
				logger.Error("Failed to update state for no-change commit: %v", err)
				return err
			}
			logger.Info("State updated to new commit (no app changes)")
		}
		return nil
	}

	if changedProjects != nil {
		logger.Info("Will reconcile %d changed project(s): %v", len(changedProjects), changedProjects)
	} else {
		logger.Info("Reconciling all projects (first deployment or change detection unavailable)")
	}

	// Update state before processing to avoid re-trying failed deployments
	// This ensures that if pre-hook or deployment fails, we don't retry the same commit on next run
	if !dryRun {
		// Store the projects we're about to process (or empty if not yet determined)
		projectsToProcess := changedProjects
		if projectsToProcess == nil {
			// We'll reconcile all projects, but we don't know the list yet
			// Update with empty list for now, will update again with actual list after reconciliation
			projectsToProcess = []string{}
		}
		if err := state.UpdateWithProjects(newCommit, projectsToProcess); err != nil {
			logger.Error("Failed to update state: %v", err)
			return err
		}
		logger.Debug("State updated to commit %s before processing", newCommit[:8])
	}

	// Create hook runner
	hookRunner := hooks.New(releaseDir, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)

	// Run pre-hook
	if err := hookRunner.RunPre(); err != nil {
		logger.Error("Pre-hook failed: %v", err)
		_ = hookRunner.RunFailure()
		return err
	}

	// Perform reconciliation
	reconciler := reconcile.New(cfg, releaseDir, dryRun)
	reconciler.SetChangedProjects(changedProjects)
	reconciledProjects, err := reconciler.Reconcile()
	if err != nil {
		logger.Error("Reconciliation failed: %v", err)
		_ = hookRunner.RunFailure()
		return err
	}

	// Atomic switch (only if not dry-run)
	if !dryRun {
		if err := atomicSwitch(newCommit, releaseDir); err != nil {
			logger.Error("Atomic switch failed: %v", err)
			_ = hookRunner.RunFailure()
			return err
		}

		// Update state with final list of reconciled projects
		if err := state.UpdateWithProjects(newCommit, reconciledProjects); err != nil {
			logger.Error("Failed to update state: %v", err)
			return err
		}
	} else {
		logger.Info("[DRY-RUN] Would switch to commit: %s", newCommit[:8])
	}

	// Run success hook using current symlink (temp directory can now be cleaned)
	if !dryRun {
		currentLink := state.GetCurrentLink()
		successHookRunner := hooks.New(currentLink, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)
		if err := successHookRunner.RunSuccess(); err != nil {
			logger.Error("Success hook failed: %v", err)
		}
	} else if err := hookRunner.RunSuccess(); err != nil {
		logger.Error("Success hook failed: %v", err)
	}

	logger.Info("Deployment complete")
	return nil
}

// Status shows the last deployment status
func Status() error {
	// Check daemon status
	statusCmd := exec.Command("systemctl", "is-active", "konta")
	output, err := statusCmd.Output()

	status := strings.TrimSpace(string(output))
	if err != nil || status != "active" {
		fmt.Printf("❌ Konta daemon is not running\n")
	} else {
		fmt.Printf("✅ Konta daemon is running\n")
	}

	fmt.Println()

	// Show last deployment info
	currentState, err := state.Load()
	if err != nil {
		logger.Debug("Failed to load state: %v", err)
	}

	if currentState.LastCommit == "" {
		fmt.Println("Last deployment: (none yet)")
	} else {
		fmt.Println("Last deployment:")
		fmt.Printf("  Commit:    %s\n", currentState.LastCommit[:8])
		fmt.Printf("  Timestamp: %s\n", currentState.LastDeployTime)
	}

	return nil
}

// atomicSwitch performs atomic switch to new release
func atomicSwitch(commit string, releaseDir string) error {
	releasesDir := state.GetReleasesDir()
	currentLink := state.GetCurrentLink()

	// Create releases directory if it doesn't exist
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		return fmt.Errorf("failed to create releases directory: %w", err)
	}

	// Move release to versioned directory
	targetDir := filepath.Join(releasesDir, commit)

	// If target already exists (idempotent), just update symlink
	if _, err := os.Stat(targetDir); err == nil {
		// Target exists, just ensure symlink points to it
		_ = os.Remove(currentLink)
		if err := os.Symlink(targetDir, currentLink); err != nil {
			return fmt.Errorf("failed to create symlink: %w", err)
		}
		logger.Info("Atomic switch completed (reused): %s", commit[:8])
		cleanupOldReleases(releasesDir, commit)
		return nil
	}

	// Target doesn't exist, move release to it
	if err := os.Rename(releaseDir, targetDir); err != nil {
		return fmt.Errorf("failed to move release directory: %w", err)
	}

	// Remove old symlink if it exists
	_ = os.Remove(currentLink)

	// Create new symlink
	if err := os.Symlink(targetDir, currentLink); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	logger.Info("Atomic switch completed: %s", commit[:8])
	cleanupOldReleases(releasesDir, commit)
	return nil
}

// cleanupOldReleases removes old release directories to avoid unused data buildup
func cleanupOldReleases(releasesDir string, currentCommit string) {
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		logger.Warn("Failed to read releases directory: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == currentCommit {
			continue
		}

		path := filepath.Join(releasesDir, name)
		if err := os.RemoveAll(path); err != nil {
			logger.Warn("Failed to remove old release %s: %v", name, err)
			continue
		}
		logger.Info("Removed old release: %s", name)
	}
}

// ManageDaemon manages the systemd service
func ManageDaemon(action string) error {
	serviceName := "konta"
	serviceFile := "/etc/systemd/system/konta.service"

	switch strings.ToLower(action) {
	case "enable":
		return daemonEnable(serviceName, serviceFile)

	case "disable":
		return daemonDisable(serviceName, serviceFile)

	case "status":
		return daemonStatus(serviceName)

	case "start":
		logger.Warn("Daemon action 'start' is deprecated. Use 'enable' instead.")
		return daemonEnable(serviceName, serviceFile)

	case "stop":
		logger.Warn("Daemon action 'stop' is deprecated. Use 'disable' instead.")
		return daemonDisable(serviceName, serviceFile)

	case "restart":
		return daemonRestart(serviceName)

	default:
		return fmt.Errorf("unknown daemon action: %s (use: enable, disable, restart, status)", action)
	}
}

func daemonEnable(serviceName, serviceFile string) error {
	// Check if we're root
	if os.Getuid() != 0 {
		return fmt.Errorf("root privileges required to enable daemon")
	}

	// Create systemd service file
	serviceContent := `[Unit]
Description=Konta GitOps for Docker Compose
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/konta run --watch
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
`

	// Write service file
	if err := os.WriteFile(serviceFile, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd daemon
	reloadCmd := exec.Command("systemctl", "daemon-reload")
	if err := reloadCmd.Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	// Enable service
	enableCmd := exec.Command("systemctl", "enable", serviceName)
	if err := enableCmd.Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	// Start service
	startCmd := exec.Command("systemctl", "start", serviceName)
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	fmt.Printf("✅ Konta daemon enabled and started\n")
	fmt.Printf("   Manage:\n")
	fmt.Printf("     konta enable     - Enable and start service\n")
	fmt.Printf("     konta disable    - Stop and disable service\n")
	fmt.Printf("     konta restart    - Restart service\n")
	fmt.Printf("     konta status     - Check status\n")
	fmt.Printf("   Logs:\n")
	fmt.Printf("     konta journal    - View live logs\n")
	fmt.Printf("     konta journal -f - Same as 'konta journal'\n")

	return nil
}

func daemonDisable(serviceName, serviceFile string) error {
	// Check if we're root
	if os.Getuid() != 0 {
		return fmt.Errorf("root privileges required to disable daemon")
	}

	// Stop service
	stopCmd := exec.Command("systemctl", "stop", serviceName)
	if err := stopCmd.Run(); err != nil {
		// Continue even if stop fails (service might not be running)
		fmt.Printf("⚠️  Failed to stop service (may not be running): %v\n", err)
	}

	// Disable service
	disableCmd := exec.Command("systemctl", "disable", serviceName)
	if err := disableCmd.Run(); err != nil {
		return fmt.Errorf("failed to disable service: %w", err)
	}

	// Remove service file
	if err := os.Remove(serviceFile); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove service file: %w", err)
		}
	}

	// Reload systemd daemon
	reloadCmd := exec.Command("systemctl", "daemon-reload")
	if err := reloadCmd.Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	fmt.Printf("✅ Konta daemon disabled\n")

	return nil
}

func daemonStatus(serviceName string) error {
	// Get service status
	statusCmd := exec.Command("systemctl", "is-active", serviceName)
	output, err := statusCmd.Output()

	if err != nil {
		fmt.Printf("❌ Konta daemon is not running\n")
		return nil
	}

	status := strings.TrimSpace(string(output))
	if status == "active" {
		fmt.Printf("✅ Konta daemon is running\n")

		// Show more details
		getStatusCmd := exec.Command("systemctl", "status", serviceName, "--no-pager")
		getStatusCmd.Stdout = os.Stdout
		getStatusCmd.Stderr = os.Stderr
		_ = getStatusCmd.Run()
	} else {
		fmt.Printf("⚠️  Konta daemon is %s\n", status)
	}

	return nil
}

func daemonStart(serviceName string) error {
	// Check if we're root
	if os.Getuid() != 0 {
		return fmt.Errorf("root privileges required to start daemon")
	}

	// Start service
	startCmd := exec.Command("systemctl", "start", serviceName)
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	fmt.Printf("✅ Konta daemon started\n")
	return nil
}

func daemonStop(serviceName string) error {
	// Check if we're root
	if os.Getuid() != 0 {
		return fmt.Errorf("root privileges required to stop daemon")
	}

	// Stop service
	stopCmd := exec.Command("systemctl", "stop", serviceName)
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	fmt.Printf("✅ Konta daemon stopped\n")
	return nil
}

func daemonRestart(serviceName string) error {
	// Check if we're root
	if os.Getuid() != 0 {
		return fmt.Errorf("root privileges required to restart daemon")
	}

	// Restart service
	restartCmd := exec.Command("systemctl", "restart", serviceName)
	if err := restartCmd.Run(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	fmt.Printf("✅ Konta daemon restarted\n")
	return nil
}
