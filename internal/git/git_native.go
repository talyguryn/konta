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
		"--depth", "1",                 // Minimal history: only current commit
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

