package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/talyguryn/konta/internal/githubdeploy"
	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/reconcile"
	"github.com/talyguryn/konta/internal/state"
	"github.com/talyguryn/konta/internal/types"
)

func markdownBlockquote(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "> deployment failed"
	}

	lines := strings.Split(text, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}

	return strings.Join(lines, "\n")
}

func shortCommitHash(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func appendAppList(lines []string, title string, apps []string) []string {
	if len(apps) == 0 {
		return lines
	}

	lines = append(lines, fmt.Sprintf("### %s", title))
	for _, app := range apps {
		lines = append(lines, fmt.Sprintf("- `%s`", app))
	}
	lines = append(lines, "")
	return lines
}

func buildSuccessComment(newCommit string, previousCommit string, compareURL string, result *types.ReconcileResult) string {
	newShort := shortCommitHash(newCommit)
	previousShort := shortCommitHash(previousCommit)

	lines := []string{
		"## Konta deployment succeeded",
		"",
		fmt.Sprintf("- Deploy of this commit `%s` succeeded.", newShort),
	}

	if previousShort != "" {
		lines = append(lines, fmt.Sprintf("- Previous stable deploy commit `%s`.", previousShort))
	}
	if compareURL != "" {
		lines = append(lines, fmt.Sprintf("- Applied edits: [view diff](%s).", compareURL))
	}

	lines = append(lines, "", "## Result", "")

	if result == nil {
		lines = append(lines, "- No app-level changes were reported.")
		return strings.Join(lines, "\n")
	}

	lines = appendAppList(lines, "Added", result.Added)
	lines = appendAppList(lines, "Updated", result.Updated)
	lines = appendAppList(lines, "Started", result.Started)
	lines = appendAppList(lines, "Removed", result.Removed)

	if len(result.Added) == 0 && len(result.Updated) == 0 && len(result.Started) == 0 && len(result.Removed) == 0 {
		lines = append(lines, "- No app-level changes were reported.")
	}

	return strings.Join(lines, "\n")
}

func reportNoProjectChangesGitHubSuccess(cfg *types.Config, previousCommit string, newCommit string) {
	if cfg == nil || !cfg.Deploy.GitHubDeployments.Enable {
		return
	}

	githubEnvironment := strings.TrimSpace(cfg.Deploy.GitHubDeployments.Environment)
	if githubEnvironment == "" {
		githubEnvironment = "production"
	}

	ghDeployClient, err := githubdeploy.New(cfg.Repository.URL, cfg.Repository.Token)
	if err != nil {
		logger.Warn("GitHub deployment status disabled: %v", err)
		return
	}

	compareURL := ghDeployClient.CompareURL(previousCommit, newCommit)
	if err := ghDeployClient.CreateCommitStatus(context.Background(), newCommit, "pending", "Konta deployment in progress", compareURL); err != nil {
		logger.Warn("Failed to create GitHub commit status (pending): %v", err)
	}

	deploymentID, err := ghDeployClient.CreateDeploymentAndMarkInProgress(context.Background(), newCommit, githubEnvironment)
	if err != nil {
		logger.Warn("Failed to create GitHub deployment status: %v", err)
		deploymentID = 0
	}

	if deploymentID != 0 {
		if err := ghDeployClient.CreateDeploymentStatus(context.Background(), deploymentID, "success", "Konta deployment succeeded (no app changes)"); err != nil {
			logger.Warn("Failed to report GitHub deployment success status: %v", err)
		}
	}

	if err := ghDeployClient.CreateCommitStatus(context.Background(), newCommit, "success", "Konta deployment succeeded (no app changes)", compareURL); err != nil {
		logger.Warn("Failed to report GitHub commit status (success): %v", err)
	}

	result := &types.ReconcileResult{}
	successComment := buildSuccessComment(newCommit, previousCommit, compareURL, result)
	if err := ghDeployClient.CreateCommitComment(context.Background(), newCommit, successComment); err != nil {
		logger.Warn("Failed to publish GitHub success comment: %v", err)
	}
}

func rollbackToStable(cfg *types.Config, stableCommit string, changedProjects []string) error {
	stableCommit = strings.TrimSpace(stableCommit)
	if stableCommit == "" {
		return fmt.Errorf("no stable commit available for rollback")
	}

	stableReleaseDir := filepath.Join(state.GetReleasesDir(), stableCommit)
	if _, err := os.Stat(stableReleaseDir); err != nil {
		return fmt.Errorf("stable release directory not found for %s: %w", stableCommit[:8], err)
	}

	logger.Warn("Starting rollback to stable release: %s", stableCommit)

	reconciler := reconcile.New(cfg, stableReleaseDir, false, stableCommit)
	if len(changedProjects) == 0 {
		logger.Warn("Rollback project scope is empty; reconciling all projects as a fallback")
	} else {
		logger.Warn("Rollback will reconcile %d affected project(s): %v", len(changedProjects), changedProjects)
	}
	reconciler.SetChangedProjects(changedProjects)
	result, err := reconciler.Reconcile()
	if err != nil {
		return fmt.Errorf("rollback reconciliation failed: %w", err)
	}

	allAffectedProjects := make([]string, 0)
	allAffectedProjects = append(allAffectedProjects, result.Updated...)
	allAffectedProjects = append(allAffectedProjects, result.Added...)
	allAffectedProjects = append(allAffectedProjects, result.Started...)

	if err := atomicSwitch(stableCommit, stableReleaseDir); err != nil {
		return fmt.Errorf("rollback switch failed: %w", err)
	}

	if err := state.UpdateWithProjects(stableCommit, allAffectedProjects); err != nil {
		return fmt.Errorf("rollback state update failed: %w", err)
	}

	logger.Warn("Rollback completed to stable release: %s", stableCommit)
	return nil
}

func rollbackProjectsForFailure(changedProjects []string, result *types.ReconcileResult) []string {
	if len(changedProjects) > 0 {
		return uniqueSortedProjects(changedProjects)
	}

	projects := make([]string, 0)
	if result != nil {
		projects = append(projects, result.Updated...)
		projects = append(projects, result.Added...)
		projects = append(projects, result.Started...)
		if strings.TrimSpace(result.Failed) != "" {
			projects = append(projects, strings.TrimSpace(result.Failed))
		}
	}

	return uniqueSortedProjects(projects)
}

func uniqueSortedProjects(projects []string) []string {
	if len(projects) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	unique := make([]string, 0, len(projects))
	for _, project := range projects {
		project = strings.TrimSpace(project)
		if project == "" || seen[project] {
			continue
		}
		seen[project] = true
		unique = append(unique, project)
	}

	sort.Strings(unique)
	return unique
}