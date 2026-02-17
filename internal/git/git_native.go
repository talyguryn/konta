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

// CloneNative clones a repository using native git command
// This is more memory-efficient than go-git for large repositories
func CloneNative(config *types.RepositoryConf, targetDir string) (string, error) {
	logger.Info("Cloning repository from %s (branch: %s) using native git", config.URL, config.Branch)

	// Clean up target directory if it exists
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.RemoveAll(targetDir); err != nil {
			return "", fmt.Errorf("failed to clean target directory: %w", err)
		}
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Prepare git clone command
	args := []string{
		"clone",
		"--depth", "5",                 // Keep last 5 commits (covers typical 1-5 commit pushes)
		"--single-branch",              // Only clone one branch
		"--branch", config.Branch,
	}

	// Add authentication token if provided
	if config.Token != "" {
		// Convert token to git credential format
		// URL should be like https://github.com/user/repo.git
		// Token is used as password with 'git' as username
		config.URL = strings.Replace(
			config.URL,
			"https://",
			"https://git:"+config.Token+"@",
			1,
		)
	}

	args = append(args, config.URL, targetDir)

	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0") // Don't prompt for password

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git clone failed: %w\n%s", err, string(output))
	}

	// Get current commit using git rev-parse
	revCmd := exec.Command("git", "rev-parse", "HEAD")
	revCmd.Dir = targetDir
	output, err = revCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}

	commit := strings.TrimSpace(string(output))
	if len(commit) != 40 {
		return "", fmt.Errorf("invalid commit hash: %s", commit)
	}

	logger.Info("Repository cloned successfully. Commit: %s", commit)
	return commit, nil
}

// GetChangedProjectsNative detects changed projects using native git diff
// GetChangedProjectsNative detects changed projects using native git diff
// This is used as fallback when go-git fails (e.g., with shallow clones)
// Works with Depth: 5 (covers typical 1-5 commit multi-pushes)
// For extremely rare cases with >5 commits in one push: falls back to "reconcile all"
func GetChangedProjectsNative(repoDir string, appsPath string, oldCommit string, newCommit string) ([]string, error) {
	logger.Debug("Using native git diff for change detection: %s..%s", oldCommit[:8], newCommit[:8])

	// Run git diff to get changed files between commits
	// Using oldCommit..newCommit range format
	cmd := exec.Command("git", "diff", "--name-only", oldCommit, newCommit)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn("Native git diff failed: %v (output: %s)", err, string(output))
		// If native git diff fails, we can't determine changes precisely
		// This can happen if oldCommit is outside Depth range (>5 commits ago)
		// Fallback: log warning and return nil (will reconcile all)
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	// Parse output to get list of changed files
	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(files) == 1 && files[0] == "" {
		// No changes
		logger.Info("Git diff found no changes between %s and %s", oldCommit[:8], newCommit[:8])
		return []string{}, nil
	}

	// Track which projects were affected
	changedProjects := make(map[string]bool)
	// Normalize appsPath to use forward slashes (git output always uses /)
	prefix := strings.TrimSuffix(strings.ReplaceAll(appsPath, "\\", "/"), "/") + "/"

	logger.Debug("Looking for changes under prefix: %s (total files: %d)", prefix, len(files))

	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}

		logger.Debug("Checking changed file: %s", file)

		// Check if the changed file is under the apps path
		if strings.HasPrefix(file, prefix) {
			// Extract project name (first directory after apps path)
			relPath := strings.TrimPrefix(file, prefix)
			parts := strings.Split(relPath, "/")
			if len(parts) > 0 && parts[0] != "" {
				projectName := parts[0]
				changedProjects[projectName] = true
				logger.Debug("Detected change in project: %s (file: %s)", projectName, file)
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(changedProjects))
	for project := range changedProjects {
		result = append(result, project)
	}

	if len(result) == 0 {
		logger.Info("Git diff found %d file changes, but none affect projects under %s", len(files), appsPath)
	} else {
		logger.Info("Detected changes in %d project(s) via native git: %v", len(result), result)
	}

	return result, nil
}