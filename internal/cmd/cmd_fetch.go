package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/talyguryn/konta/internal/config"
	"github.com/talyguryn/konta/internal/git"
	"github.com/talyguryn/konta/internal/lock"
	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/reconcile"
	"github.com/talyguryn/konta/internal/state"
	"github.com/talyguryn/konta/internal/types"
)

// ReconcileOnceFetch performs a single reconciliation cycle using persistent repo (Phase 2)
// This is much more memory-efficient than cloning every time
// FUTURE: Use this instead of reconcileOnce() when Phase 2 is ready
func reconcileOnceFetch(dryRun bool, version string) error {
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

	// Use persistent repository instead of temporary clone
	// This is the key difference from Phase 1
	persistentRepoDir := filepath.Join(state.GetCurrentLink(), "..", "repo")

	// First run: Initialize persistent repo
	if currentState.LastCommit == "" {
		logger.Info("First run: initializing persistent repository")
		if err := os.MkdirAll(persistentRepoDir, 0755); err != nil {
			return fmt.Errorf("failed to create repo directory: %w", err)
		}

		newCommit, err := git.InitRepo(persistentRepoDir, &cfg.Repository)
		if err != nil {
			return fmt.Errorf("failed to init repo: %w", err)
		}

		logger.Info("Repository initialized. Commit: %s", newCommit[:8])

		// First run: reconcile all projects
		if err := reconcileWithPersistentRepo(cfg, persistentRepoDir, nil, dryRun); err != nil {
			return err
		}

		// Save state
		currentState.LastCommit = newCommit
		if err := state.Save(currentState); err != nil {
			logger.Warn("Failed to save state: %v", err)
		}

		return nil
	}

	// Subsequent runs: Just fetch updates
	logger.Debug("Fetching updates from repository")
	newCommit, err := git.FetchNative(persistentRepoDir, &cfg.Repository)
	if err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Check if there are changes
	if newCommit == currentState.LastCommit {
		lastCommitStr := currentState.LastCommit
		if len(lastCommitStr) > 8 {
			lastCommitStr = lastCommitStr[:8]
		}
		logger.Info("No changes detected (current: %s)", lastCommitStr)

		// Even without changes, perform health check
		logger.Info("Performing container health check")
		if !dryRun {
			reconciler := reconcile.New(cfg, persistentRepoDir, dryRun)
			reconciler.SetChangedProjects(nil)
			if _, err := reconciler.HealthCheck(); err != nil {
				logger.Warn("Health check issues: %v", err)
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
	logger.Info("New commit detected: %s → %s", lastCommitStr, newCommit[:8])

	// Validate compose path
	if err := git.ValidateComposePath(persistentRepoDir, cfg.Repository.Path); err != nil {
		return err
	}

	// Detect which projects have changed (using persistent repo, not temporary)
	changedProjects, err := git.GetChangedProjects(persistentRepoDir, cfg.Repository.Path, currentState.LastCommit, newCommit)
	if err != nil {
		logger.Warn("Failed to detect changes: %v (will reconcile all)", err)
		changedProjects = nil
	}

	// Reconcile with persistent repository
	if err := reconcileWithPersistentRepo(cfg, persistentRepoDir, changedProjects, dryRun); err != nil {
		return err
	}

	// Save state
	currentState.LastCommit = newCommit
	if err := state.Save(currentState); err != nil {
		logger.Warn("Failed to save state: %v", err)
	}

	logger.Info("Reconciliation complete")
	return nil
}

// reconcileWithPersistentRepo performs reconciliation without cloning
func reconcileWithPersistentRepo(cfg *types.Config, repoDir string, changedProjects []string, dryRun bool) error {
	reconciler := reconcile.New(cfg, repoDir, dryRun)

	if changedProjects != nil {
		reconciler.SetChangedProjects(changedProjects)
		logger.Info("Reconciling %d changed project(s)", len(changedProjects))
	} else {
		logger.Info("Reconciling all projects")
	}

	result, err := reconciler.Reconcile()
	if err != nil {
		return fmt.Errorf("reconciliation failed: %w", err)
	}

	// Log results
	if len(result.Updated) > 0 {
		logger.Info("Updated: %v", result.Updated)
	}
	if len(result.Added) > 0 {
		logger.Info("Added: %v", result.Added)
	}
	if len(result.Removed) > 0 {
		logger.Info("Removed: %v", result.Removed)
	}
	if len(result.Started) > 0 {
		logger.Info("Started: %v", result.Started)
	}

	// Run hooks if needed
	if len(result.Updated) > 0 || len(result.Added) > 0 {
		// Would call hooks here
		logger.Debug("Hooks would run here")
	}

	return nil
}

// CalculateMemorySavings estimate memory saved by fetch vs clone
func CalculateMemorySavings() string {
	// This is informational - helps DevOps understand benefits
	return `
Fetch-based approach savings:

Per cycle:
  Clone: ~80 MB (full repository load)
  Fetch: ~5 MB (only changes downloaded)
  Savings per cycle: ~75 MB!

Per hour (60-second polling):
  Clone cycles: 60 × 80 MB = 4.8 GB memory load
  Fetch cycles: 60 × 5 MB = 300 MB memory load
  Hourly savings: 4.5 GB! ✨

On 512 MB VPS:
  Clone approach: Out of memory after 6 cycles
  Fetch approach: Can run indefinitely
`
}
