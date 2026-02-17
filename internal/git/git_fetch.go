package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/types"
)

// InitRepo initializes or opens a persistent repository for fetching
// This is used in watch mode to avoid cloning every cycle
func InitRepo(repoDir string, config *types.RepositoryConf) (string, error) {
	logger.Info("Initializing persistent repository at %s", repoDir)

	// Check if repo already exists
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		// Repo exists, just get current commit
		logger.Debug("Repository already initialized, updating...")
		return FetchNative(repoDir, config)
	}

	// Clone fresh repository (only on first run)
	logger.Info("Creating initial repository clone from %s", config.URL)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Initialize empty repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = repoDir
	if err := initCmd.Run(); err != nil {
		return "", fmt.Errorf("git init failed: %w", err)
	}

	// Add remote
	remoteCmd := exec.Command("git", "remote", "add", "origin", config.URL)
	remoteCmd.Dir = repoDir
	if err := remoteCmd.Run(); err != nil {
		return "", fmt.Errorf("git remote add failed: %w", err)
	}

	// First fetch
	return FetchNative(repoDir, config)
}

// FetchNative updates an existing repository using native git
// Much more efficient than cloning - only downloads changes
func FetchNative(repoDir string, config *types.RepositoryConf) (string, error) {
	logger.Debug("Fetching updates from remote repository")

	// Prepare auth if token provided
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if config.Token != "" {
		// GIT_ASKPASS is more reliable than modifying URL
		env = append(env, "GIT_ASKPASS=/bin/true")
	}

	// Fetch
	fetchCmd := exec.Command("git", "fetch", "origin", config.Branch, "--depth", "1")
	fetchCmd.Dir = repoDir
	fetchCmd.Env = env

	output, err := fetchCmd.CombinedOutput()
	if err != nil && !strings.Contains(err.Error(), "exit status") {
		logger.Warn("Git fetch warning: %v, output: %s", err, string(output))
		// Continue - fetch may warn but still succeed
	}

	// Reset to origin/branch
	resetCmd := exec.Command("git", "reset", "--hard", "origin/"+config.Branch)
	resetCmd.Dir = repoDir

	if err := resetCmd.Run(); err != nil {
		return "", fmt.Errorf("git reset failed: %w", err)
	}

	// Get current commit
	commit, err := GetCurrentCommitNative(repoDir)
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	logger.Debug("Repository updated to: %s", commit[:8])
	return commit, nil
}

// GetCurrentCommitNative gets the current commit using native git
func GetCurrentCommitNative(repoDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}

	commit := strings.TrimSpace(string(output))
	if len(commit) != 40 {
		return "", fmt.Errorf("invalid commit hash: %s", commit)
	}

	return commit, nil
}

// GetCommitMessage gets commit message for logging
func GetCommitMessage(repoDir string, commit string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%B", commit)
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	message := strings.TrimSpace(string(output))
	if len(message) > 100 {
		message = message[:100] + "..."
	}

	return message, nil
}

// CleanupRepo removes old objects to save space
func CleanupRepo(repoDir string) error {
	logger.Debug("Cleaning up git repository")

	// Remove unreachable objects
	gcCmd := exec.Command("git", "gc", "--aggressive", "--prune=now")
	gcCmd.Dir = repoDir

	if err := gcCmd.Run(); err != nil {
		logger.Warn("Git gc failed (non-critical): %v", err)
		// Don't fail on cleanup
	}

	return nil
}

// GetDiffStats returns stats about file changes
func GetDiffStats(repoDir string, oldCommit string, newCommit string) (int, error) {
	if oldCommit == "" {
		return 0, nil
	}

	cmd := exec.Command("git", "diff", "--stat", oldCommit+".."+newCommit)
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(output), "\n")
	return len(lines) - 1, nil // -1 for last empty line
}
