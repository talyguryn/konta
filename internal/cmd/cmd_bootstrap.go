package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/talyguryn/konta/internal/config"
	"github.com/talyguryn/konta/internal/dockerutil"
	"github.com/talyguryn/konta/internal/git"
	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/state"
	"github.com/talyguryn/konta/internal/types"
)

// Bootstrap performs first-time setup with optional CLI parameters
// Usage: konta bootstrap [--repo URL] [--path PATH] [--branch BRANCH] [--interval SECONDS] [--token TOKEN] [--konta_updates auto|notify|false] [--release_channel stable|next]
func Bootstrap(args []string) error {
	if os.Getuid() != 0 {
		return fmt.Errorf("bootstrap requires root privileges. Please run: sudo konta bootstrap")
	}
	logger.Info("Starting Konta bootstrap")

	// Parse command-line arguments
	var (
		repoURL        string
		branch         string
		appsPath       string
		interval       int
		token          string
		kontaUpdates   string
		releaseChannel string
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
		case "--release_channel":
			if i+1 < len(args) {
				releaseChannel = args[i+1]
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
	if releaseChannel == "" {
		releaseChannel = "stable"
	}
	releaseChannel = strings.ToLower(strings.TrimSpace(releaseChannel))
	if releaseChannel != "stable" && releaseChannel != "next" {
		releaseChannel = "stable"
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
			ProjectNameHashMode:         "rolling_only",
			RollingHealthTimeoutSeconds: 300,
			RollingHealthRetries:        1,
			SelfHeal: types.SelfHealConf{
				Enable:   true,
				MaxRetry: 0,
			},
			GitHubDeployments: types.GitHubDeploymentsConf{
				Enable:      true,
				Environment: "production",
			},
		},
		Logging: types.LoggingConf{
			Level: "info",
		},
		ReleaseChannel: releaseChannel,
		KontaUpdates:   kontaUpdates,
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
	fmt.Println("✓ Setup complete!")
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Printf("  Repository: %s\n", repoURL)
	fmt.Printf("  Branch:     %s\n", branch)
	fmt.Printf("  Base path:  %s\n", appsPath)
	fmt.Printf("  Interval:   %d seconds\n", interval)
	fmt.Printf("  Auto-update: %s\n", kontaUpdates)
	fmt.Printf("  Release channel: %s\n", releaseChannel)

	fmt.Println()
	fmt.Println("Starting daemon...")
	if err := daemonEnable(daemonManager()); err != nil {
		logger.Warn("Failed to auto-enable daemon: %v", err)
		fmt.Printf("\n⚠  Could not auto-start daemon. To enable it manually, run:\n")
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
	fmt.Print("Release channel [stable/next] [stable]: ")
	releaseChannel, _ := reader.ReadString('\n')
	releaseChannel = strings.ToLower(strings.TrimSpace(releaseChannel))
	if releaseChannel == "" {
		releaseChannel = "stable"
	}
	if releaseChannel != "stable" && releaseChannel != "next" {
		releaseChannel = "stable"
		fmt.Printf("Invalid channel, using default: %s\n", releaseChannel)
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
			ProjectNameHashMode:         "rolling_only",
			RollingHealthTimeoutSeconds: 300,
			RollingHealthRetries:        1,
			SelfHeal: types.SelfHealConf{
				Enable:   true,
				MaxRetry: 0,
			},
			GitHubDeployments: types.GitHubDeploymentsConf{
				Enable:      true,
				Environment: "production",
			},
		},
		Logging: types.LoggingConf{
			Level: "info",
		},
		ReleaseChannel: releaseChannel,
		KontaUpdates:   kontaUpdates,
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
	fmt.Println("\n✓ Setup complete!")
	fmt.Println("\nStarting daemon...")

	// Automatically enable and start daemon
	if err := daemonEnable(daemonManager()); err != nil {
		logger.Warn("Failed to auto-enable daemon: %v", err)
		fmt.Printf("\n⚠  Could not auto-start daemon. To enable it manually, run:\n")
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

	// 1. Stop and disable daemon
	fmt.Println("1. Stopping Konta daemon...")
	if runtime.GOOS == "darwin" {
		plistPath := "/Library/LaunchDaemons/com.talyguryn.konta.plist"
		_ = exec.Command("launchctl", "unload", "-w", plistPath).Run()
		if _, err := os.Stat(plistPath); err == nil {
			if err := os.Remove(plistPath); err != nil {
				logger.Warn("Failed to remove launchd plist: %v", err)
			} else {
				fmt.Printf("   ✓ Removed launchd plist\n")
			}
		} else {
			fmt.Println("   (daemon not installed)")
		}
	} else {
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
	}
	fmt.Println()

	// 2. Stop and remove all Konta-managed containers
	fmt.Println("2. Stopping Konta-managed containers...")
	cmd := dockerutil.Command("ps", "-aq", "--filter", "label=konta.managed=true")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		containerIDs := strings.Fields(string(output))
		if len(containerIDs) > 0 {
			fmt.Printf("   Found %d containers\n", len(containerIDs))
			stopCmd := dockerutil.Command("stop")
			stopCmd.Args = append(stopCmd.Args, containerIDs...)
			_ = stopCmd.Run()

			rmCmd := dockerutil.Command("rm")
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
