package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	// Clone the repository with minimal history
	// Depth: 1 means we only get current commit, no history
	// This is sufficient since we only compare commits for changes
	repo, err := gogit.PlainClone(targetDir, false, &gogit.CloneOptions{
		URL:           config.URL,
		ReferenceName: plumbing.NewBranchReferenceName(config.Branch),
		SingleBranch:  true,
		Depth:         1, // Minimal history: only current commit needed for change detection
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

// GetChangedProjects returns the list of projects that changed between two commits
// Returns nil (reconcile all) if oldCommit is empty
// Returns empty slice if no changes detected
// Returns nil with error if detection fails
func GetChangedProjects(repoDir string, appsPath string, oldCommit string, newCommit string) ([]string, error) {
	// If no previous commit, all projects are considered changed
	if oldCommit == "" {
		return nil, nil // First deployment
	}

	if oldCommit == newCommit {
		return []string{}, nil // Same commit, no changes
	}

	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	// Get old commit object
	oldHash := plumbing.NewHash(oldCommit)
	oldCommitObj, err := repo.CommitObject(oldHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get old commit %s: %w", oldCommit, err)
	}

	// Get new commit object
	newHash := plumbing.NewHash(newCommit)
	newCommitObj, err := repo.CommitObject(newHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get new commit %s: %w", newCommit, err)
	}

	// Get tree objects
	oldTree, err := oldCommitObj.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get old tree: %w", err)
	}

	newTree, err := newCommitObj.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get new tree: %w", err)
	}

	// Get changes between trees
	changes, err := oldTree.Diff(newTree)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}

	// Track which projects were affected
	changedProjects := make(map[string]bool)
	// Git paths always use forward slashes, normalize appsPath
	prefix := strings.TrimSuffix(strings.ReplaceAll(appsPath, "\\", "/"), "/") + "/"

	logger.Debug("Looking for changes under prefix: %s", prefix)

	for _, change := range changes {
		// Check both "from" and "to" paths in case of renames
		paths := []string{change.From.Name, change.To.Name}

		for _, path := range paths {
			if path == "" {
				continue
			}

			logger.Debug("Checking changed file: %s", path)

			// Check if the changed file is under the apps path (git always uses /)
			if strings.HasPrefix(path, prefix) {
				// Extract project name (first directory after apps path)
				relPath := strings.TrimPrefix(path, prefix)
				parts := strings.Split(relPath, "/") // Git always uses forward slash
				if len(parts) > 0 && parts[0] != "" {
					projectName := parts[0]
					changedProjects[projectName] = true
					logger.Debug("Detected change in project: %s (file: %s)", projectName, path)
				}
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(changedProjects))
	for project := range changedProjects {
		result = append(result, project)
	}

	if len(result) == 0 {
		logger.Info("Git diff found %d file changes, but none affect projects under %s", len(changes), appsPath)
	} else {
		logger.Info("Detected changes in %d project(s): %v", len(result), result)
	}

	return result, nil
}

