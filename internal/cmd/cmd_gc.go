package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/talyguryn/konta/internal/dockerutil"
	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/state"
)

func cleanupOldReleases(releasesDir string, keepCommits ...string) {
	keep := make(map[string]bool)
	for _, c := range keepCommits {
		c = strings.TrimSpace(c)
		if c != "" {
			keep[c] = true
		}
	}

	// Preserve commits referenced by state.json (global and per-project state).
	for _, c := range stateCommitsToKeep() {
		keep[c] = true
	}

	// Add any release directories currently mounted by konta.managed containers
	mountedReleaseDirs, err := mountedReleaseDirsInUse(releasesDir)
	if err != nil {
		logger.Warn("Failed to resolve mounted release dirs in GC: %v (skipping cleanup for safety)", err)
		return // Fail-safe: skip cleanup if we can't safely determine what's in use
	}
	for releaseDirName := range mountedReleaseDirs {
		keep[releaseDirName] = true
	}

	if len(keep) == 0 {
		logger.Debug("Skipping release cleanup: no commits to keep specified and no mounted release dirs detected")
		return
	}

	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		logger.Warn("Failed to read releases directory: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if keep[name] {
			continue
		}

		path := filepath.Join(releasesDir, name)
		if err := os.RemoveAll(path); err != nil {
			logger.Warn("Failed to remove old release %s: %v", name, err)
			continue
		}
		// Log at INFO for actual commit releases, DEBUG for temp dirs
		if strings.HasPrefix(name, "temp-") {
			logger.Debug("Removed old release: %s", name)
		} else {
			logger.Info("Removed old release: %s", name)
		}
	}
}

func stateCommitsToKeep() []string {
	st, err := state.Load()
	if err != nil {
		logger.Warn("Failed to load state for release retention: %v", err)
		return nil
	}

	seen := make(map[string]bool)
	commits := make([]string, 0)
	appendCommit := func(commit string) {
		commit = strings.TrimSpace(commit)
		if commit == "" || seen[commit] {
			return
		}
		seen[commit] = true
		commits = append(commits, commit)
	}

	appendCommit(st.LastCommit)
	appendCommit(st.LastAttemptedCommit)

	for _, p := range st.Projects {
		appendCommit(p.LastCommit)
		appendCommit(p.ActiveCommit)
	}

	return commits
}

// mountedReleaseDirsInUse returns a set of release directory names (commit hashes)
// that are currently referenced by bind-mounts in konta.managed containers.
// This ensures that containers which still read files from old releases will have
// those releases preserved by GC.
func mountedReleaseDirsInUse(releasesDir string) (map[string]bool, error) {
	kept := make(map[string]bool)

	listCmd := dockerutil.Command("ps", "-aq", "--filter", "label=konta.managed=true")
	listOutput, err := listCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}

	containerIDs := strings.Fields(strings.TrimSpace(string(listOutput)))
	if len(containerIDs) == 0 {
		return kept, nil
	}

	type inspectMount struct {
		Type   string `json:"Type"`
		Source string `json:"Source"`
	}
	type inspectContainer struct {
		Mounts []inspectMount `json:"Mounts"`
	}

	inspectArgs := append([]string{"inspect"}, containerIDs...)
	inspectCmd := dockerutil.Command(inspectArgs...)
	inspectOutput, err := inspectCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker inspect failed: %w", err)
	}

	containers := make([]inspectContainer, 0)
	if err := json.Unmarshal(inspectOutput, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse docker inspect output: %w", err)
	}

	cleanReleasesDir := filepath.Clean(releasesDir)
	for _, container := range containers {
		for _, mount := range container.Mounts {
			if strings.TrimSpace(mount.Type) != "bind" {
				continue
			}

			source := filepath.Clean(strings.TrimSpace(mount.Source))
			if source == "" {
				continue
			}

			relToReleases, relErr := filepath.Rel(cleanReleasesDir, source)
			if relErr != nil {
				continue
			}

			relToReleases = filepath.ToSlash(relToReleases)
			if relToReleases == "." || strings.HasPrefix(relToReleases, "../") {
				continue
			}

			parts := strings.Split(relToReleases, "/")
			if len(parts) == 0 {
				continue
			}

			releaseDirName := strings.TrimSpace(parts[0])
			if releaseDirName != "" {
				kept[releaseDirName] = true
			}
		}
	}

	return kept, nil
}

// detectChangedProjectsBySnapshot compares app project directories between the
// current active release and a newly cloned release.
// This is used as a conservative fallback when git diff cannot be computed
// (for example, shallow history gaps after long daemon downtime).
func detectChangedProjectsBySnapshot(currentReleaseDir string, newReleaseDir string, appsPath string) ([]string, error) {
	currentAppsDir := filepath.Join(currentReleaseDir, appsPath)
	newAppsDir := filepath.Join(newReleaseDir, appsPath)

	currentProjects, err := listProjectsWithCompose(currentAppsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list current projects: %w", err)
	}

	newProjects, err := listProjectsWithCompose(newAppsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list new projects: %w", err)
	}

	allProjectNames := make(map[string]bool)
	for project := range currentProjects {
		allProjectNames[project] = true
	}
	for project := range newProjects {
		allProjectNames[project] = true
	}

	changed := make([]string, 0)
	for project := range allProjectNames {
		currentProjectDir, inCurrent := currentProjects[project]
		newProjectDir, inNew := newProjects[project]

		if !inCurrent || !inNew {
			changed = append(changed, project)
			continue
		}

		currentHash, err := dirContentHash(currentProjectDir)
		if err != nil {
			return nil, fmt.Errorf("failed to hash current project %s: %w", project, err)
		}

		newHash, err := dirContentHash(newProjectDir)
		if err != nil {
			return nil, fmt.Errorf("failed to hash new project %s: %w", project, err)
		}

		if currentHash != newHash {
			changed = append(changed, project)
		}
	}

	sort.Strings(changed)
	return changed, nil
}

func listProjectsWithCompose(appsDir string) (map[string]string, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, err
	}

	projects := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectName := entry.Name()
		projectDir := filepath.Join(appsDir, projectName)
		composePath := filepath.Join(projectDir, "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			projects[projectName] = projectDir
		}
	}

	return projects, nil
}

func dirContentHash(dir string) (string, error) {
	trackedFiles, trackedErr := listGitTrackedFiles(dir)
	if trackedErr == nil {
		hasher := sha256.New()
		for _, relPath := range trackedFiles {
			_, _ = hasher.Write([]byte("F:" + relPath + "\n"))

			fileData, readErr := os.ReadFile(filepath.Join(dir, relPath))
			if readErr != nil {
				return "", readErr
			}

			fileHash := sha256.Sum256(fileData)
			_, _ = hasher.Write(fileHash[:])
		}

		return hex.EncodeToString(hasher.Sum(nil)), nil
	}

	logger.Debug("Failed to list git-tracked files for %s, falling back to full directory hash: %v", dir, trackedErr)

	hasher := sha256.New()

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			return nil
		}

		if strings.Contains(relPath, "/.git/") || strings.HasPrefix(relPath, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			_, _ = hasher.Write([]byte("D:" + relPath + "\n"))
			return nil
		}

		_, _ = hasher.Write([]byte("F:" + relPath + "\n"))

		fileData, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		fileHash := sha256.Sum256(fileData)
		_, _ = hasher.Write(fileHash[:])
		return nil
	})
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func listGitTrackedFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "-C", dir, "ls-files", "--", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	files := make([]string, 0)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		line = filepath.ToSlash(line)
		if strings.HasPrefix(line, "./") {
			line = strings.TrimPrefix(line, "./")
		}
		files = append(files, line)
	}

	sort.Strings(files)
	return files, nil
}
