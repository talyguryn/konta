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
  konta install                 First-time setup
  konta uninstall               Remove Konta completely
  konta run [--dry-run] [--watch]  Execute once or in watch mode
  konta daemon [enable|disable|status]  Manage systemd service
  konta status                  Show last deployment info
  konta journal                 View live logs (journalctl -f)
  konta update                  Update to latest version from GitHub
  konta version (-v)            Show version
  konta help (-h)               Show this help

Short flags:
  -h, --help                    Show this help
  -v, --version                 Show version
  -r                            Same as 'run'
  -d                            Same as 'daemon enable'
  -s                            Same as 'status'

Examples:
  konta install              # Interactive setup
  konta run                  # Single reconciliation
  konta run --watch          # Watch mode (poll every N seconds)
  konta run --dry-run        # Show what would change
  konta daemon enable        # Enable background service
  konta daemon disable       # Disable background service
  konta daemon status        # Check if running
  konta journal              # View live logs
  konta update               # Update to latest version

More info: https://github.com/talyguryn/konta
`, version)
}

// Install performs first-time setup
func Install() error {
	logger.Info("Starting Konta installation")

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

	fmt.Print("Apps path inside repo [vps0/apps]: ")
	appsPath, _ := reader.ReadString('\n')
	appsPath = strings.TrimSpace(appsPath)
	if appsPath == "" {
		appsPath = "vps0/apps"
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
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Run once to test:     konta run")
	fmt.Println("  2. Enable daemon:        sudo konta daemon enable")
	fmt.Println("  3. Check daemon status:  systemctl status konta")
	fmt.Println("  4. View logs:            journalctl -u konta -f")

	// Ask if user wants to enable daemon now
	fmt.Print("\nEnable daemon now? (yes/no) [yes]: ")
	daemonReader := bufio.NewReader(os.Stdin)
	answer, _ := daemonReader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "" || answer == "yes" || answer == "y" {
		fmt.Println("Attempting to enable daemon...")
		if err := daemonEnable("konta", "/etc/systemd/system/konta.service"); err != nil {
			logger.Error("Failed to enable daemon: %v", err)
			fmt.Printf("\n⚠️  Could not auto-enable daemon. Run as root later:\n")
			fmt.Printf("    sudo konta daemon enable\n")
		}
	}

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
func Update(currentVersion string) error {
	fmt.Printf("Current version: v%s\n", currentVersion)
	fmt.Println("Checking for updates from GitHub...")

	// Fetch latest release from GitHub API
	resp, err := http.Get("https://api.github.com/repos/talyguryn/konta/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse release info: %v", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	fmt.Printf("Latest version: v%s\n", latestVersion)

	if latestVersion == currentVersion {
		fmt.Println("✅ Already running the latest version!")
		return nil
	}

	fmt.Printf("\n🎉 New version available: v%s\n", latestVersion)
	fmt.Print("Download and install? (yes/no): ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "yes" && answer != "y" {
		fmt.Println("Update cancelled")
		return nil
	}

	// Determine the correct binary name based on OS/ARCH
	binaryName := fmt.Sprintf("konta-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		binaryName = "konta-linux"
	}

	// Find the asset URL
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("\nDownloading %s...\n", binaryName)

	// Download the new binary
	resp, err = http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	// Download to temp file
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

	// Make executable
	if err := os.Chmod(tmpFile, 0755); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to set permissions: %v", err)
	}

	// Backup current binary
	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to backup current binary: %v", err)
	}

	// Move new binary to place
	if err := os.Rename(tmpFile, exePath); err != nil {
		// Try to restore backup
		_ = os.Rename(backupPath, exePath)
		return fmt.Errorf("failed to install new binary: %v", err)
	}

	// Remove backup
	_ = os.Remove(backupPath)

	fmt.Printf("\n✅ Updated to v%s successfully!\n", latestVersion)
	fmt.Println("\nIf you have the daemon running, restart it:")
	fmt.Println("  sudo systemctl restart konta")

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

		// First reconciliation already done above, now enter polling loop
		var ticker *time.Ticker
		ticker = time.NewTicker(time.Duration(cfg.Repository.Interval) * time.Second)
		defer ticker.Stop()

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
		return nil
	}

	if changedProjects != nil {
		logger.Info("Will reconcile %d changed project(s): %v", len(changedProjects), changedProjects)
	} else {
		logger.Info("Reconciling all projects (first deployment or change detection unavailable)")
	}

	// Create hook runner
	hookRunner := hooks.New(releaseDir, cfg.Hooks.Pre, cfg.Hooks.Success, cfg.Hooks.Failure)

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

		// Update state with reconciled projects
		if err := state.UpdateWithProjects(newCommit, reconciledProjects); err != nil {
			logger.Error("Failed to update state: %v", err)
			return err
		}
	} else {
		logger.Info("[DRY-RUN] Would switch to commit: %s", newCommit[:8])
	}

	// Run success hook
	if err := hookRunner.RunSuccess(); err != nil {
		logger.Error("Success hook failed: %v", err)
	}

	logger.Info("Deployment complete")
	return nil
}

// Status shows the last deployment status
func Status() error {
	currentState, err := state.Load()
	if err != nil {
		logger.Error("Failed to load state: %v", err)
	}

	if currentState.LastCommit == "" {
		fmt.Println("No deployments yet")
		return nil
	}

	fmt.Println("Last deployment:")
	fmt.Printf("  Commit:    %s\n", currentState.LastCommit[:8])
	fmt.Printf("  Timestamp: %s\n", currentState.LastDeployTime)

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
	return nil
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

	default:
		return fmt.Errorf("unknown daemon action: %s (use: enable, disable, status)", action)
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
	fmt.Printf("     systemctl start konta   - Start service\n")
	fmt.Printf("     systemctl stop konta    - Stop service\n")
	fmt.Printf("     systemctl status konta  - Check status\n")
	fmt.Printf("   Logs:\n")
	fmt.Printf("     journalctl -u konta -f  - Live logs\n")

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
