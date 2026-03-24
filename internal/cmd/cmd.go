package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/talyguryn/konta/internal/config"
	"github.com/talyguryn/konta/internal/git"
	"github.com/talyguryn/konta/internal/githubdeploy"
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
	konta bootstrap [OPTIONS]
	konta uninstall
	konta run [--dry-run] [--watch]
	konta deploy [--dry-run]
	konta daemon [enable|disable|restart|status]
	konta enable | konta disable | konta restart | konta status
	konta journal
	konta config [-e]
	konta update [-y]
	konta version (-v)
	konta help (-h)

Bootstrap Options:
  --repo URL                        GitHub repository URL (required)
  --path PATH                       Base path in repo (contains 'apps' dir, default: repo root)
  --branch BRANCH                   Git branch (default: main)
  --interval SECONDS                Polling interval (default: 120)
  --token TOKEN                     GitHub token (or set KONTA_TOKEN env)
	--release_channel stable|next     Konta release channel for updates (default: stable)

Short flags:
  -h, --help                        Show this help
  -v, --version                     Show version
  -r                                Same as 'run'
  -d                                Same as 'daemon enable' (or 'start')
  -s                                Show daemon status
  -j                                Show live logs (same as 'journal')

Update flags:
	-y                                Skip confirmation and auto-update
	--channel stable|next             Override release channel for this update command

Examples:
  konta bootstrap                     # Interactive setup
  konta bootstrap --repo https://github.com/user/infra
  konta bootstrap --repo https://github.com/talyguryn/konta --path spb
  konta run                         # Single reconciliation
  konta run --watch                 # Watch mode (poll every N seconds)
  konta run --dry-run               # Show what would change
	konta deploy                      # Force full redeploy for latest commit
  konta start                       # Start the daemon
  konta stop                        # Stop the daemon
  konta restart                     # Restart the daemon
  konta status                      # Check daemon status
  konta journal                     # View live logs
  konta journal -f                  # Same as 'konta journal'
  konta update                      # Update to latest version (interactive)
	konta update -y                   # Update without confirmation
	konta update --channel next       # Update from experimental next channel

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

// Bootstrap, installInteractive, validateInstallParams, testRepositoryConnection,
// Uninstall → moved to cmd_bootstrap.go

// Journal shows live logs
func Journal() error {
	var cmd *exec.Cmd
	fmt.Println("Showing live logs (Ctrl+C to exit)...")
	fmt.Println()
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("tail", "-f", "/var/log/konta/konta.log")
	} else {
		cmd = exec.Command("journalctl", "-u", "konta", "-f")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// Run executes reconciliation once or in watch mode
func Run(dryRun bool, watch bool, version string) error {
	// Load config to get hook paths
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Run started hook when konta daemon starts
	if watch {
		// Get current state to determine repo directory
		if err := state.Init(); err != nil {
			return err
		}
		currentState, err := state.Load()
		if err != nil || currentState.LastCommit == "" {
			logger.Debug("No previous deployment, skipping started hook")
		} else {
			currentLink := state.GetCurrentLink()
			startedHookRunner := hooks.New(currentLink, cfg.Hooks.StartedAbs, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)
			if err := startedHookRunner.RunStarted(); err != nil {
				logger.Warn("Started hook failed: %v", err)
			}
		}
	}

	// Execute reconciliation once
	if err := reconcileOnce(dryRun, version, true, false); err != nil && !watch {
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
			_ = CheckForUpdates(version, cfg.KontaUpdates, cfg.ReleaseChannel)
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
				_ = CheckForUpdates(version, cfg.KontaUpdates, cfg.ReleaseChannel)
			}

			if err := reconcileOnce(false, version, false, false); err != nil {
				logger.Error("Deployment error: %v", err)
				// Continue on error, don't exit
			}

			// Aggressive garbage collection for low-memory environments
			// Multiple GC passes help release more memory from go-git objects
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			allocBefore := ms.Alloc

			// First GC pass
			runtime.GC()
			debug.FreeOSMemory()

			// Second pass if memory is still high
			runtime.ReadMemStats(&ms)
			if ms.Alloc > 50*1024*1024 { // If over 50MB
				runtime.GC()
				debug.FreeOSMemory()
				runtime.GC() // Third pass for stubborn memory
			}

			// Log memory stats for debugging
			runtime.ReadMemStats(&ms)
			if allocBefore > 30*1024*1024 {
				logger.Debug("Memory optimization: %d MB → %d MB", allocBefore/1024/1024, ms.Alloc/1024/1024)
			}
		}
	}

	return nil
}

// Deploy performs a forced full redeploy on the latest commit.
// Unlike Run, it does not rely on changed project detection and reconciles all projects.
func Deploy(dryRun bool, version string) error {
	return reconcileOnce(dryRun, version, true, true)
}

// reconcileOnce performs a single reconciliation cycle
func reconcileOnce(dryRun bool, version string, isFirstRun bool, forceFullRedeploy bool) error {
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
	lastSuccessfulCommit := currentState.LastCommit
	if currentReleaseCommit, currentReleaseErr := state.GetCurrentReleaseCommit(); currentReleaseErr == nil {
		lastSuccessfulCommit = currentReleaseCommit
	} else if currentState.LastCommit != "" {
		logger.Debug("Failed to get current release commit from symlink, using state fallback: %v", currentReleaseErr)
	}
	stableRollbackCommit := strings.TrimSpace(lastSuccessfulCommit)
	activeCommitForCleanup := stableRollbackCommit
	if activeCommitForCleanup == "" {
		activeCommitForCleanup = currentState.LastCommit
	}
	defer func() {
		cleanupOldReleases(state.GetReleasesDir(), activeCommitForCleanup, stableRollbackCommit)
	}()

	// Clone the repository into a temp directory, then immediately promote to stable versioned path.
	// Resolve latest commit hash upfront (no clone needed yet).
	// This allows us to skip cloning entirely when the release dir already exists.
	newCommit, err := git.ResolveLatestCommit(&cfg.Repository)
	if err != nil {
		return fmt.Errorf("failed to resolve latest commit: %w", err)
	}

	// Clone directly into the stable versioned release directory — no temp dir ever created.
	// Docker bind mounts will always reference this stable path.
	releaseDir := filepath.Join(state.GetReleasesDir(), newCommit)
	if _, statErr := os.Stat(releaseDir); statErr != nil {
		// Release dir doesn't exist yet — clone directly into it
		if _, err := git.Clone(&cfg.Repository, releaseDir); err != nil {
			return err
		}
		logger.Debug("Cloned into stable release directory: %s", newCommit[:8])
	} else {
		logger.Debug("Reusing existing stable release directory: %s", newCommit[:8])
	}

	defer func() {
		if dryRun {
			return
		}

		appsDirForPrune := filepath.Join(releaseDir, cfg.Repository.Path)
		desiredProjects, desiredErr := listDesiredProjectsForStatePrune(appsDirForPrune)
		if desiredErr != nil {
			resolvedCurrentDir, resolveErr := filepath.EvalSymlinks(state.GetCurrentLink())
			if resolveErr == nil {
				fallbackAppsDir := filepath.Join(resolvedCurrentDir, cfg.Repository.Path)
				desiredProjects, desiredErr = listDesiredProjectsForStatePrune(fallbackAppsDir)
			}
		}
		if desiredErr != nil {
			logger.Warn("Failed to collect desired projects for state prune: %v", desiredErr)
			return
		}

		if err := state.PruneProjects(desiredProjects); err != nil {
			logger.Warn("Failed to prune stale project state: %v", err)
		}
	}()

	// Check if there are changes
	if newCommit == currentState.LastCommit {
		lastCommitStr := currentState.LastCommit
		if len(lastCommitStr) > 8 {
			lastCommitStr = lastCommitStr[:8]
		}
		if forceFullRedeploy {
			logger.Info("No new commit detected (current: %s), but force deploy is enabled", lastCommitStr)
		} else {
			logger.Info("No changes detected (current: %s)", lastCommitStr)
		}

		if !forceFullRedeploy {
			// Even without changes, perform health check to ensure containers are running
			logger.Info("Performing container health check")
			if !dryRun {
				reconciler := reconcile.New(cfg, releaseDir, dryRun, newCommit)
				reconciler.SetChangedProjects(nil) // nil means check all projects
				if _, err := reconciler.HealthCheck(); err != nil {
					logger.Warn("Health check encountered issues: %v", err)
					// Don't return error, just warn
				}
			}

			// Ensure current symlink points to the latest known commit even without changes.
			if !dryRun {
				if err := atomicSwitch(newCommit, releaseDir); err != nil {
					logger.Error("Atomic switch failed: %v", err)
					return err
				}
			} else {
				logger.Info("[DRY-RUN] Would switch to commit: %s", newCommit[:8])
			}
			return nil
		}
	}

	if newCommit != currentState.LastCommit {
		lastCommitStr := currentState.LastCommit
		if len(lastCommitStr) > 8 {
			lastCommitStr = lastCommitStr[:8]
		} else if lastCommitStr == "" {
			lastCommitStr = "none"
		}
		logger.Info("New commit detected: %s -> %s", lastCommitStr, newCommit[:8])
	} else if forceFullRedeploy {
		logger.Info("Force full redeploy enabled for current commit: %s", newCommit[:8])
	}

	if !forceFullRedeploy && !dryRun && !isFirstRun && strings.TrimSpace(currentState.LastAttemptedCommit) == newCommit && currentState.LastAttemptStatus == "failure" {
		logger.Warn("Skipping automatic redeploy for previously failed commit %s", newCommit[:8])
		return nil
	}

	// Validate compose path
	if err := git.ValidateComposePath(releaseDir, cfg.Repository.Path); err != nil {
		return err
	}

	var changedProjects []string
	if forceFullRedeploy {
		changedProjects = nil // nil means reconcile all
		logger.Info("Force full redeploy: reconciling all projects")
	} else {
		// Detect which projects have changed
		changedProjects, err = git.GetChangedProjects(releaseDir, cfg.Repository.Path, currentState.LastCommit, newCommit)
		if err != nil {
			logger.Warn("Failed to detect changed projects via git diff: %v", err)

			currentReleaseDir, currentReleaseErr := filepath.EvalSymlinks(state.GetCurrentLink())
			if currentReleaseErr != nil {
				logger.Warn("Snapshot diff fallback unavailable (cannot resolve current release): %v (will reconcile all)", currentReleaseErr)
				changedProjects = nil // nil means reconcile all
			} else {
				snapshotChangedProjects, snapshotErr := detectChangedProjectsBySnapshot(currentReleaseDir, releaseDir, cfg.Repository.Path)
				if snapshotErr != nil {
					logger.Warn("Snapshot diff fallback failed: %v (will reconcile all)", snapshotErr)
					changedProjects = nil // nil means reconcile all
				} else {
					changedProjects = snapshotChangedProjects
					logger.Info("Snapshot diff fallback detected %d changed project(s): %v", len(changedProjects), changedProjects)
				}
			}
		}
	}

	if changedProjects != nil && len(changedProjects) == 0 {
		logger.Info("No project changes detected in %s, but cleaning up orphans", cfg.Repository.Path)

		// Even with no project changes, we should clean up orphan containers
		// that may have been moved out of the apps directory
		if !dryRun {
			reconciler := reconcile.New(cfg, releaseDir, dryRun, newCommit)
			if err := reconciler.CleanupOrphans(); err != nil {
				logger.Warn("Failed to cleanup orphans: %v", err)
				// Don't fail on orphan cleanup, just warn
			}
		}

		if !dryRun {
			// Keep current symlink aligned with latest commit, even if no app changes.
			if err := atomicSwitch(newCommit, releaseDir); err != nil {
				logger.Error("Atomic switch failed: %v", err)
				return err
			}
			if err := state.UpdateWithProjects(newCommit, []string{}); err != nil {
				logger.Error("Failed to update state for no-change commit: %v", err)
				return err
			}
			reportNoProjectChangesGitHubSuccess(cfg, lastSuccessfulCommit, newCommit)
			activeCommitForCleanup = newCommit
			logger.Info("State updated to new commit (no app changes)")
		} else {
			logger.Info("[DRY-RUN] Would switch to commit: %s", newCommit[:8])
		}
		return nil
	}

	if changedProjects != nil {
		logger.Info("Will reconcile %d changed project(s): %v", len(changedProjects), changedProjects)
	} else {
		logger.Info("Reconciling all projects (first deployment or change detection unavailable)")
	}

	var ghDeployClient *githubdeploy.Client
	var ghDeploymentID int64
	githubCompareURL := ""
	stableCommitURL := ""
	reportedFailure := false
	if !dryRun && cfg.Deploy.GitHubDeployments.Enable {
		githubEnvironment := strings.TrimSpace(cfg.Deploy.GitHubDeployments.Environment)
		if githubEnvironment == "" {
			githubEnvironment = "production"
		}

		ghDeployClient, err = githubdeploy.New(cfg.Repository.URL, cfg.Repository.Token)
		if err != nil {
			logger.Warn("GitHub deployment status disabled: %v", err)
		} else {
			githubCompareURL = ghDeployClient.CompareURL(lastSuccessfulCommit, newCommit)
			stableCommitURL = ghDeployClient.CommitURL(stableRollbackCommit)
			if err := ghDeployClient.CreateCommitStatus(context.Background(), newCommit, "pending", "Konta deployment in progress", githubCompareURL); err != nil {
				logger.Warn("Failed to create GitHub commit status (pending): %v", err)
			}

			ghDeploymentID, err = ghDeployClient.CreateDeploymentAndMarkInProgress(context.Background(), newCommit, githubEnvironment)
			if err != nil {
				logger.Warn("Failed to create GitHub deployment status: %v", err)
				ghDeploymentID = 0
			} else {
				logger.Info("GitHub deployment started (id=%d, environment=%s)", ghDeploymentID, githubEnvironment)
			}
		}
	}

	reportGitHubFailure := func(reason string, rollbackNote string, rollbackCompleted bool) {
		if reportedFailure {
			return
		}
		reportedFailure = true
		if !dryRun {
			if err := state.MarkAttempt(newCommit, "failure"); err != nil {
				logger.Warn("Failed to persist failed deployment attempt: %v", err)
			}
		}
		if ghDeployClient == nil {
			return
		}

		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "deployment failed"
		}
		failedCommitShort := newCommit
		if len(failedCommitShort) > 8 {
			failedCommitShort = failedCommitShort[:8]
		}
		lastSuccessfulCommitShort := strings.TrimSpace(lastSuccessfulCommit)
		if len(lastSuccessfulCommitShort) > 8 {
			lastSuccessfulCommitShort = lastSuccessfulCommitShort[:8]
		}
		if ghDeploymentID != 0 {
			if err := ghDeployClient.CreateDeploymentStatus(context.Background(), ghDeploymentID, "failure", "konta: "+reason); err != nil {
				logger.Warn("Failed to report GitHub deployment failure status: %v", err)
			}
		}

		if err := ghDeployClient.CreateCommitStatus(context.Background(), newCommit, "failure", "konta: "+reason, githubCompareURL); err != nil {
			logger.Warn("Failed to report GitHub commit status (failure): %v", err)
		}

		commentLines := []string{
			"## Konta deployment failed",
			"",
			markdownBlockquote(reason),
			"",
			"## Result",
			"",
			fmt.Sprintf("- Deploy of this commit `%s` failed.", failedCommitShort),
		}
		if lastSuccessfulCommitShort != "" {
			commentLines = append(commentLines, fmt.Sprintf("- Last successful deploy commit `%s`.", lastSuccessfulCommitShort))
		}
		if rollbackCompleted && stableCommitURL != "" {
			stableLabel := "stable commit"
			if lastSuccessfulCommitShort != "" {
				stableLabel = fmt.Sprintf("stable commit `%s`", lastSuccessfulCommitShort)
			}
			commentLines = append(commentLines, fmt.Sprintf("- Rollback completed to [%s](%s).", stableLabel, stableCommitURL))
		} else if rollbackCompleted {
			commentLines = append(commentLines, "- Rollback completed to stable commit.")
		} else if strings.TrimSpace(rollbackNote) != "" {
			commentLines = append(commentLines, fmt.Sprintf("- %s", rollbackNote))
		}
		if githubCompareURL != "" {
			commentLines = append(commentLines, "", fmt.Sprintf("See not applied edits: [view diff](%s).", githubCompareURL))
		}

		if err := ghDeployClient.CreateCommitComment(context.Background(), newCommit, strings.Join(commentLines, "\n")); err != nil {
			logger.Warn("Failed to publish GitHub failure comment: %v", err)
		}
	}

	var reconciledResult *types.ReconcileResult
	allAffectedProjects := make([]string, 0)

	attemptRollback := func(rollbackProjects []string) (string, bool) {
		if dryRun {
			return "", false
		}
		if stableRollbackCommit == "" {
			logger.Warn("Automatic rollback skipped: no stable successful release commit found")
			return "Rollback skipped: no stable successful release commit found.", false
		}
		if err := rollbackToStable(cfg, stableRollbackCommit, rollbackProjects); err != nil {
			logger.Error("Rollback failed: %v", err)
			return fmt.Sprintf("Rollback failed: %v", err), false
		}
		return fmt.Sprintf("Rollback completed to stable commit `%s`.", stableRollbackCommit), true
	}

	if !dryRun {
		if err := state.MarkAttempt(newCommit, "in_progress"); err != nil {
			logger.Warn("Failed to persist deployment attempt state: %v", err)
		}
	}

	// Create hook runner
	hookRunner := hooks.New(releaseDir, cfg.Hooks.StartedAbs, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)

	// Run pre-hook
	if err := hookRunner.RunPre(); err != nil {
		logger.Error("Pre-hook failed: %v", err)
		_ = hookRunner.RunFailure(fmt.Sprintf("Pre-hook failed: %v", err))
		reportGitHubFailure(fmt.Sprintf("Pre-hook failed: %v", err), "", false)
		return err
	}

	// Perform reconciliation
	reconciler := reconcile.New(cfg, releaseDir, dryRun, newCommit)

	// Optionally ensure projects marked with konta.recreate=true are always reconciled.
	// This is a manual override for projects that need guaranteed cleanup on each cycle.
	if changedProjects != nil {
		recreateProjects, err := findProjectsMarkedForRecreate(filepath.Join(releaseDir, cfg.Repository.Path))
		if err != nil {
			logger.Debug("Failed to find konta.recreate projects: %v", err)
		} else if len(recreateProjects) > 0 {
			for _, project := range recreateProjects {
				if !contains(changedProjects, project) {
					changedProjects = append(changedProjects, project)
				}
			}
			changedProjects = uniqueSortedProjects(changedProjects)
			logger.Debug("Added %d konta.recreate project(s) to reconcile list: %v", len(recreateProjects), recreateProjects)
		}
	}

	reconciler.SetChangedProjects(changedProjects)
	result, err := reconciler.Reconcile()
	reconciledResult = result
	if err != nil {
		logger.Error("Reconciliation failed: %v", err)
		_ = hookRunner.RunFailure(fmt.Sprintf("Reconciliation failed: %v", err))
		rollbackProjects := rollbackProjectsForFailure(changedProjects, reconciledResult)
		rollbackNote, rollbackCompleted := attemptRollback(rollbackProjects)
		reportGitHubFailure(fmt.Sprintf("Reconciliation failed: %v", err), rollbackNote, rollbackCompleted)
		return err
	}

	// Collect all affected projects for state tracking
	allAffectedProjects = append(allAffectedProjects, result.Updated...)
	allAffectedProjects = append(allAffectedProjects, result.Added...)
	allAffectedProjects = append(allAffectedProjects, result.Started...)
	allAffectedProjects = uniqueSortedProjects(allAffectedProjects)

	// Atomic switch (only if not dry-run)
	if !dryRun {
		if err := atomicSwitch(newCommit, releaseDir); err != nil {
			logger.Error("Atomic switch failed: %v", err)
			_ = hookRunner.RunFailure(fmt.Sprintf("Atomic switch failed: %v", err))
			rollbackNote, rollbackCompleted := attemptRollback(allAffectedProjects)
			reportGitHubFailure(fmt.Sprintf("Atomic switch failed: %v", err), rollbackNote, rollbackCompleted)
			return err
		}

		// Update state with final list of reconciled projects
		if err := state.UpdateWithProjects(newCommit, allAffectedProjects); err != nil {
			logger.Error("Failed to update state: %v", err)
			rollbackNote, rollbackCompleted := attemptRollback(allAffectedProjects)
			reportGitHubFailure(fmt.Sprintf("Failed to update state: %v", err), rollbackNote, rollbackCompleted)
			return err
		}
		activeCommitForCleanup = newCommit
	} else {
		logger.Info("[DRY-RUN] Would switch to commit: %s", newCommit[:8])
	}

	// Run success hook using current symlink (temp directory can now be cleaned)
	if !dryRun {
		currentLink := state.GetCurrentLink()
		successHookRunner := hooks.New(currentLink, cfg.Hooks.StartedAbs, cfg.Hooks.PreAbs, cfg.Hooks.SuccessAbs, cfg.Hooks.FailureAbs, cfg.Hooks.PostUpdateAbs)
		if err := successHookRunner.RunSuccess(result); err != nil {
			logger.Error("Success hook failed: %v", err)
		}
	} else if err := hookRunner.RunSuccess(result); err != nil {
		logger.Error("Success hook failed: %v", err)
	}

	if ghDeployClient != nil && ghDeploymentID != 0 {
		if err := ghDeployClient.CreateDeploymentStatus(context.Background(), ghDeploymentID, "success", "Konta deployment succeeded"); err != nil {
			logger.Warn("Failed to report GitHub deployment success status: %v", err)
		}
	}
	if ghDeployClient != nil {
		if err := ghDeployClient.CreateCommitStatus(context.Background(), newCommit, "success", "Konta deployment succeeded", githubCompareURL); err != nil {
			logger.Warn("Failed to report GitHub commit status (success): %v", err)
		}

		successComment := buildSuccessComment(newCommit, lastSuccessfulCommit, githubCompareURL, result)
		if err := ghDeployClient.CreateCommitComment(context.Background(), newCommit, successComment); err != nil {
			logger.Warn("Failed to publish GitHub success comment: %v", err)
		}
	}

	logger.Info("Deployment complete")
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
		logger.Debug("Atomic switch completed (reused): %s", commit[:8])
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

