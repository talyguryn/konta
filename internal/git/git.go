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
	// Depth: 5 means we get last 5 commits for change detection
	// This covers typical multi-commit pushes (1-5 commits) while keeping memory minimal (14-16 MB)
	// For edge cases with >5 commits: fallback to native git fetch (git_native.go)
	repo, err := gogit.PlainClone(targetDir, false, &gogit.CloneOptions{
		URL:           config.URL,
		ReferenceName: plumbing.NewBranchReferenceName(config.Branch),
		SingleBranch:  true,
		Depth:         5, // Balance: covers 1-5 commits + minimal memory
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
// Uses GetChangedProjectsNative as fallback if go-git fails
// Fallback fetches oldCommit from remote if needed, ensuring accurate detection with minimal memory
// This design: shallow clone (Depth: 1) for minimal memory, explicit fetch for accuracy
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
		// Fallback to native git diff if go-git can't find the commit
		// This happens with shallow clones when oldCommit is outside the depth range
		logger.Debug("go-git failed to find commit %s (shallow clone?), falling back to native git diff", oldCommit[:8])
		return GetChangedProjectsNative(repoDir, appsPath, oldCommit, newCommit)
	}

	// Get new commit object
	newHash := plumbing.NewHash(newCommit)
	newCommitObj, err := repo.CommitObject(newHash)
	if err != nil {
		logger.Debug("go-git failed to find commit %s, falling back to native git diff", newCommit[:8])
		return GetChangedProjectsNative(repoDir, appsPath, oldCommit, newCommit)
	}

	// Get tree objects
	oldTree, err := oldCommitObj.Tree()
	if err != nil {
		logger.Debug("go-git failed to get tree, falling back to native git diff")
		return GetChangedProjectsNative(repoDir, appsPath, oldCommit, newCommit)
	}

	newTree, err := newCommitObj.Tree()
	if err != nil {
		logger.Debug("go-git failed to get tree, falling back to native git diff")
		return GetChangedProjectsNative(repoDir, appsPath, oldCommit, newCommit)
	}

	// Get changes between trees
	changes, err := oldTree.Diff(newTree)
	if err != nil {
		logger.Debug("go-git diff failed, falling back to native git diff: %v", err)
		return GetChangedProjectsNative(repoDir, appsPath, oldCommit, newCommit)
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

