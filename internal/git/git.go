package git

import (
	"fmt"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/types"
)

// Clone clones a git repository
func Clone(config *types.RepositoryConf, targetDir string) (string, error) {
	logger.Info("Cloning repository from %s (branch: %s)", config.URL, config.Branch)

	// Clean up target directory if it exists
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.RemoveAll(targetDir); err != nil {
			return "", fmt.Errorf("failed to clean target directory: %w", err)
		}
	}

	// Prepare auth options
	var auth *http.BasicAuth
	if config.Token != "" {
		auth = &http.BasicAuth{
			Username: "git",
			Password: config.Token,
		}
	}

	// Clone the repository
	repo, err := gogit.PlainClone(targetDir, false, &gogit.CloneOptions{
		URL:           config.URL,
		ReferenceName: plumbing.NewBranchReferenceName(config.Branch),
		SingleBranch:  true,
		Depth:         1,
		Auth:          auth,
	})
	if err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// Get the current commit hash
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD reference: %w", err)
	}

	commit := ref.Hash().String()
	logger.Info("Repository cloned successfully. Commit: %s", commit)

	return commit, nil
}

// Fetch fetches updates from remote (for existing repo)
func Fetch(repoDir string, config *types.RepositoryConf) (string, error) {
	logger.Info("Opening repository at %s", repoDir)

	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	// Prepare auth options
	var auth *http.BasicAuth
	if config.Token != "" {
		auth = &http.BasicAuth{
			Username: "git",
			Password: config.Token,
		}
	}

	logger.Info("Fetching updates...")
	if err := repo.Fetch(&gogit.FetchOptions{
		RemoteName: "origin",
		Auth:       auth,
	}); err != nil && err != gogit.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}

	// Get the remote reference
	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", config.Branch), true)
	if err != nil {
		return "", fmt.Errorf("failed to get remote reference: %w", err)
	}

	commit := remoteRef.Hash().String()
	return commit, nil
}

// Reset resets the repository to a specific commit
func Reset(repoDir string, commitHash string) error {
	logger.Info("Resetting repository to %s", commitHash)

	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	hash := plumbing.NewHash(commitHash)
	if err := wt.Reset(&gogit.ResetOptions{
		Mode:   gogit.HardReset,
		Commit: hash,
	}); err != nil {
		return fmt.Errorf("failed to reset: %w", err)
	}

	logger.Info("Repository reset to %s", commitHash)
	return nil
}

// GetCurrentCommit returns the current commit hash
func GetCurrentCommit(repoDir string) (string, error) {
	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD reference: %w", err)
	}

	return ref.Hash().String(), nil
}

// ValidateComposePath validates that the apps path exists and contains compose files
func ValidateComposePath(repoDir string, appsPath string) error {
	appsDir := filepath.Join(repoDir, appsPath)

	if _, err := os.Stat(appsDir); err != nil {
		return fmt.Errorf("apps path does not exist: %s", appsDir)
	}

	return nil
}
