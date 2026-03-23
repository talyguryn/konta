package reconcile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/state"
	"github.com/talyguryn/konta/internal/types"
)

// Reconciler manages the reconciliation process
type Reconciler struct {
	config          *types.Config
	repoDir         string
	dryRun          bool
	appsDir         string
	deployCommit    string
	changedProjects map[string]bool // Track which projects have changes
}

// New creates a new reconciler
func New(config *types.Config, repoDir string, dryRun bool, deployCommit string) *Reconciler {
	return &Reconciler{
		config:          config,
		repoDir:         repoDir,
		dryRun:          dryRun,
		appsDir:         filepath.Join(repoDir, config.Repository.Path),
		deployCommit:    strings.TrimSpace(deployCommit),
		changedProjects: make(map[string]bool),
	}
}

// SetChangedProjects configures which projects have changes and should be reconciled
func (r *Reconciler) SetChangedProjects(projects []string) {
	if projects == nil {
		// nil means reconcile all projects (first run or error detecting changes)
		r.changedProjects = nil
		logger.Debug("Reconciler configured to process all projects")
		return
	}

	r.changedProjects = make(map[string]bool)
	for _, project := range projects {
		r.changedProjects[project] = true
	}
	logger.Debug("Reconciler configured to process %d specific projects: %v", len(projects), projects)
}

// Reconcile performs the reconciliation
// Returns detailed information about what was updated, added, removed, etc.
func (r *Reconciler) Reconcile() (*types.ReconcileResult, error) {
	logger.Info("Starting reconciliation")

	result := &types.ReconcileResult{
		Updated: []string{},
		Added:   []string{},
		Removed: []string{},
		Started: []string{},
	}

	// Get desired projects from git
	desired, err := r.getDesiredProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get desired projects: %w", err)
	}

	logger.Info("Found %d desired projects", len(desired))

	// Get currently running projects (only Konta-managed ones)
	running, err := r.getRunningProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get running projects: %w", err)
	}

	logger.Info("Found %d running Konta-managed projects", len(running))

	removedOrphans := r.cleanupOrphanProjects(desired, running)
	result.Removed = append(result.Removed, removedOrphans...)

	// Track which projects were reconciled
	reconciledProjects := []string{}

	// Reconcile desired projects
	for _, project := range desired {
		// Skip projects that haven't changed (unless changedProjects is nil, meaning reconcile all)
		if r.changedProjects != nil && !r.changedProjects[project] {
			logger.Info("Skipping project %s (no changes detected)", project)
			continue
		}

		// Check if project is new or existing.
		// A running rolling stack (<app>-<8hex>) means the app already exists
		// and should be classified as Updated, not Added.
		isNew := !isProjectPresentInRunning(project, running)

		if err := r.reconcileProject(project); err != nil {
			result.Failed = project
			return result, fmt.Errorf("failed to reconcile project %s: %w", project, err)
		}
		reconciledProjects = append(reconciledProjects, project)

		// Categorize the action
		if isNew {
			result.Added = append(result.Added, project)
		} else {
			result.Updated = append(result.Updated, project)
		}
	}

	// Ensure all desired projects have their containers running.
	// Handles two cases: stopped containers (docker start) and fully absent
	// containers — e.g. when a rolling stack was accidentally killed — which
	// requires a full reconcile so the correct (possibly hash-named) stack is
	// brought back up.
	for _, project := range desired {
		// Skip if we already reconciled this project
		if contains(reconciledProjects, project) {
			continue
		}

		// If the project has no containers at all, run a full reconcile so that
		// rolling naming, health checks, and label application are all handled.
		if !r.hasAnyContainersForApp(project) {
			logger.Info("Project %s has no containers, running full reconcile to restore it", project)
			if err := r.reconcileProject(project); err != nil {
				logger.Warn("Failed to restore project %s: %v", project, err)
			} else {
				reconciledProjects = append(reconciledProjects, project)
				result.Started = append(result.Started, project)
			}
			continue
		}

		// Check if any containers are stopped for this project
		hasStoppedContainers, err := r.hasStoppedContainers(project)
		if err != nil {
			logger.Warn("Failed to check containers for project %s: %v", project, err)
			continue
		}

		if hasStoppedContainers {
			logger.Info("Project %s has stopped containers, starting them", project)
			if err := r.startProject(project); err != nil {
				logger.Warn("Failed to start project %s: %v", project, err)
				// Don't return error, just warn - let other projects continue
			} else {
				reconciledProjects = append(reconciledProjects, project)
				result.Started = append(result.Started, project)
			}
		}
	}

	logger.Info("Reconciliation complete")
	return result, nil
}

// HealthCheck ensures all desired containers are running (used when no code changes detected)
func (r *Reconciler) HealthCheck() ([]string, error) {
	logger.Info("Starting container health check")
	if !r.config.Deploy.SelfHeal.Enable {
		logger.Info("Self-heal is disabled (deploy.self_heal.enable=false): health check will not auto-reconcile drift or restarts")
	}

	// Get desired projects from git
	desired, err := r.getDesiredProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get desired projects: %w", err)
	}

	logger.Debug("Checking health of %d desired projects", len(desired))

	// Track which projects were started
	startedProjects := []string{}

	// Check if all desired projects have their containers running.
	// Also recover projects whose containers were fully removed (e.g. a rolling
	// stack wiped by a previous bug or manual intervention).
	for _, project := range desired {
		// Fully missing → full reconcile (handles rolling naming correctly)
		if !r.hasAnyContainersForApp(project) {
			if !r.allowSelfHealAttempt(project, "containers are missing") {
				continue
			}
			r.recordSelfHealAttempt(project, "containers are missing")

			logger.Info("Project %s has no containers, running full reconcile to restore it", project)
			if err := r.reconcileProject(project); err != nil {
				logger.Warn("Failed to restore project %s: %v", project, err)
			} else {
				startedProjects = append(startedProjects, project)
			}
			continue
		}

		hasStoppedContainers, err := r.hasStoppedContainers(project)
		if err != nil {
			logger.Warn("Failed to check containers for project %s: %v", project, err)
			continue
		}

		if hasStoppedContainers {
			if !r.allowSelfHealAttempt(project, "stopped containers") {
				continue
			}
			r.recordSelfHealAttempt(project, "stopped containers")

			logger.Info("Project %s has stopped containers, starting them", project)
			if err := r.startProject(project); err != nil {
				logger.Warn("Failed to start project %s: %v", project, err)
				// Don't return error, just warn - let other projects continue
			} else {
				startedProjects = append(startedProjects, project)
			}
			continue
		}

		hasUnhealthyContainers, err := r.hasUnhealthyContainers(project)
		if err != nil {
			logger.Warn("Failed to check unhealthy containers for project %s: %v", project, err)
			continue
		}

		if hasUnhealthyContainers {
			if !r.allowSelfHealAttempt(project, "unhealthy containers") {
				continue
			}
			r.recordSelfHealAttempt(project, "unhealthy containers")

			logger.Warn("Project %s has unhealthy containers, running full reconcile", project)
			if err := r.reconcileProject(project); err != nil {
				logger.Warn("Failed to recover unhealthy project %s: %v", project, err)
			} else {
				startedProjects = append(startedProjects, project)
			}
			continue
		}

		hasDrift, driftReason, err := r.hasDeploymentDrift(project)
		if err != nil {
			logger.Warn("Failed to check deployment drift for project %s: %v", project, err)
			continue
		}

		if hasDrift {
			if !r.allowSelfHealAttempt(project, fmt.Sprintf("deployment drift: %s", driftReason)) {
				continue
			}
			r.recordSelfHealAttempt(project, fmt.Sprintf("deployment drift: %s", driftReason))

			logger.Warn("Project %s has deployment drift (%s), running full reconcile", project, driftReason)
			if err := r.reconcileProject(project); err != nil {
				logger.Warn("Failed to recover drifted project %s: %v", project, err)
			} else {
				startedProjects = append(startedProjects, project)
			}
		}
	}

	// Remove orphan projects (only Konta-managed ones with konta.managed=true label)
	// This ensures orphans are cleaned up even when no code changes are detected.
	// Rolling stacks (<app>-<hash>) that belong to a desired app are NOT orphans.
	running, err := r.getRunningProjects()
	if err != nil {
		logger.Warn("Failed to get running projects: %v", err)
	} else {
		r.cleanupOrphanProjects(desired, running)
	}

	logger.Info("Health check complete")
	return startedProjects, nil
}

func (r *Reconciler) allowSelfHealAttempt(project string, reason string) bool {
	if !r.config.Deploy.SelfHeal.Enable {
		logger.Warn("Skipping self-heal for project %s (%s): disabled by config", project, reason)
		return false
	}

	maxRetry := r.config.Deploy.SelfHeal.MaxRetry
	if maxRetry <= 0 {
		return true
	}

	attempts, err := state.GetProjectSelfHealAttempts(project)
	if err != nil {
		logger.Warn("Failed to read self-heal attempts for project %s: %v", project, err)
		return false
	}

	if attempts >= maxRetry {
		logger.Warn("Skipping self-heal for project %s (%s): max retry reached (%d/%d)", project, reason, attempts, maxRetry)
		return false
	}

	return true
}

func (r *Reconciler) recordSelfHealAttempt(project string, reason string) {
	attempts, err := state.IncrementProjectSelfHealAttempts(project)
	if err != nil {
		logger.Warn("Failed to persist self-heal attempt for project %s (%s): %v", project, reason, err)
		return
	}

	maxRetry := r.config.Deploy.SelfHeal.MaxRetry
	if maxRetry <= 0 {
		logger.Info("Self-heal attempt #%d for project %s (%s)", attempts, project, reason)
		return
	}

	logger.Info("Self-heal attempt #%d/%d for project %s (%s)", attempts, maxRetry, project, reason)
}

// CleanupOrphans removes Konta-managed containers that are no longer in the apps configuration
// This is useful when there are repository changes but no changes in the apps directory
func (r *Reconciler) CleanupOrphans() error {
	logger.Info("Starting orphan cleanup")

	// Get desired projects from git
	desired, err := r.getDesiredProjects()
	if err != nil {
		return fmt.Errorf("failed to get desired projects: %w", err)
	}

	// Get Konta-managed running projects
	running, err := r.getRunningProjects()
	if err != nil {
		return fmt.Errorf("failed to get running projects: %w", err)
	}

	// Remove orphan projects (only Konta-managed ones).
	// Rolling stacks (<app>-<hash>) that belong to a desired app are NOT orphans.
	r.cleanupOrphanProjects(desired, running)

	logger.Info("Orphan cleanup complete")
	return nil
}

func (r *Reconciler) cleanupOrphanProjects(desired []string, running []string) []string {
	removed := make([]string, 0)

	for _, project := range running {
		if isDesiredOrRollingStack(project, desired) {
			continue
		}

		logger.Info("Removing orphan Konta-managed project: %s", project)
		if r.dryRun {
			logger.Info("[DRY-RUN] Would remove project: %s", project)
			continue
		}

		if err := r.downProject(project); err != nil {
			logger.Error("Failed to remove project %s: %v", project, err)
			continue
		}

		removed = append(removed, project)
	}

	return removed
}

func (r *Reconciler) getDesiredProjects() ([]string, error) {
	entries, err := os.ReadDir(r.appsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var projects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		composePath := filepath.Join(r.appsDir, entry.Name(), "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			projects = append(projects, entry.Name())
		}
	}

	sort.Strings(projects)
	return projects, nil
}

func (r *Reconciler) getRunningProjects() ([]string, error) {
	// Get all Konta-managed projects (including stopped containers).
	// For rolling stacks prefer base app label (konta.app) so desired-vs-running comparison remains stable.
	cmd := exec.Command("docker", "ps", "-a", "--filter", "label=konta.managed=true", "--format", "{{.Label \"konta.app\"}}|{{.Label \"com.docker.compose.project\"}}")
	output, err := cmd.Output()
	if err != nil {
		logger.Warn("Failed to get running projects: %v", err)
		return []string{}, nil
	}

	projects := []string{}
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}

		appName := strings.TrimSpace(parts[0])
		composeProject := strings.TrimSpace(parts[1])
		projectKey := composeProject
		if appName != "" {
			projectKey = appName
		}

		if projectKey != "" && !seen[projectKey] {
			seen[projectKey] = true
			projects = append(projects, projectKey)
		}
	}

	sort.Strings(projects)
	return projects, nil
}

func (r *Reconciler) reconcileProject(project string) error {
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")

	logger.Info("Reconciling project: %s", project)

	rollingEnabled, err := r.composeHasLabel(composePath, "konta.rolling=true")
	if err != nil {
		return fmt.Errorf("failed to inspect rolling label for project %s: %w", project, err)
	}

	useProjectHashName := r.shouldUseHashInProjectName(rollingEnabled)
	targetProjectName := project
	if useProjectHashName {
		shortCommit := r.shortDeployCommit()
		if shortCommit == "" {
			return fmt.Errorf("deploy commit is required when hash-based project naming is enabled")
		}
		targetProjectName = fmt.Sprintf("%s-%s", project, shortCommit)
	}

	if err := r.handleProjectModeMigration(project, targetProjectName, rollingEnabled); err != nil {
		return err
	}

	if !rollingEnabled {
		hasLegacyStack, err := r.hasStack(project, project)
		if err != nil {
			return fmt.Errorf("failed to inspect existing non-rolling stack for project %s: %w", project, err)
		}

		if hasLegacyStack {
			logger.Info("Restarting non-rolling project %s before compose up to free host-bound resources", project)
			if !r.dryRun {
				if err := r.downComposeProjectWithContext(project, composePath, filepath.Join(r.appsDir, project), false); err != nil {
					return fmt.Errorf("failed to restart non-rolling project %s before compose up: %w", project, err)
				}
			}
		}
	}

	if r.dryRun {
		logger.Info("[DRY-RUN] Would run docker compose for %s (target stack: %s)", project, targetProjectName)
		return nil
	}

	cmd := exec.Command(
		"docker", "compose",
		"-p", targetProjectName,
		"-f", composePath,
		"up", "-d",
		"--remove-orphans",
	)

	cmd.Dir = filepath.Join(r.appsDir, project)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stderr
	cmd.Stderr = &stderr
	// Add Konta management labels to all containers in this stack
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_LABELS=konta.managed=true,konta.app=%s,konta.commit=%s", project, r.shortDeployCommit()))

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()

		// Check if error is due to container name conflict
		if strings.Contains(stderrStr, "already in use by container") {
			logger.Warn("Container name conflict detected, attempting cleanup")

			// Try to remove conflicting containers by forcing down with project name
			// This handles renamed projects (e.g., example-web -> konta-web)
			if cleanupErr := r.cleanupConflictingContainers(project); cleanupErr != nil {
				logger.Warn("Cleanup failed: %v", cleanupErr)
			}

			// Retry docker compose up
			cmd = exec.Command(
				"docker", "compose",
				"-p", targetProjectName,
				"-f", composePath,
				"up", "-d",
				"--remove-orphans",
			)
			cmd.Dir = filepath.Join(r.appsDir, project)
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_LABELS=konta.managed=true,konta.app=%s,konta.commit=%s", project, r.shortDeployCommit()))

			if retryErr := cmd.Run(); retryErr != nil {
				return fmt.Errorf("docker compose failed after cleanup retry: %w (original: %v)", retryErr, stderrStr)
			}

			logger.Info("Successfully resolved container name conflict")
		} else {
			// Not a conflict error, return original error with stderr
			return fmt.Errorf("docker compose failed: %w\nStderr: %s", err, stderrStr)
		}
	}

	if rollingEnabled {
		hasHealthcheck, err := r.composeHasHealthcheck(composePath)
		if err != nil {
			_ = r.downComposeProjectWithContext(targetProjectName, composePath, filepath.Join(r.appsDir, project), true)
			return fmt.Errorf("failed to inspect healthcheck for rolling project %s: %w", project, err)
		}

		if !hasHealthcheck {
			logger.Warn("Rolling deployment for project %s has no healthcheck defined. Verifying containers are stably running before cleanup. Consider adding a healthcheck for safer rolling deployments.", project)
			if err := r.waitForProjectRunningWithRetries(targetProjectName, r.config.Deploy.RollingHealthTimeoutSeconds, r.config.Deploy.RollingHealthRetries); err != nil {
				_ = r.downComposeProjectWithContext(targetProjectName, composePath, filepath.Join(r.appsDir, project), true)
				return fmt.Errorf("rolling deployment runtime check failed for project %s: %w", project, err)
			}
		} else {
			if err := r.waitForProjectHealthyWithRetries(targetProjectName, r.config.Deploy.RollingHealthTimeoutSeconds, r.config.Deploy.RollingHealthRetries); err != nil {
				_ = r.downComposeProjectWithContext(targetProjectName, composePath, filepath.Join(r.appsDir, project), true)
				return fmt.Errorf("rolling deployment healthcheck failed for project %s: %w", project, err)
			}
		}
	}

	if err := r.cleanupOldStacksForApp(project, targetProjectName, composePath, filepath.Join(r.appsDir, project)); err != nil {
		logger.Warn("Failed to cleanup old stacks for project %s: %v", project, err)
	}

	// After successful compose up, immediately stop containers marked with konta.stopped=true
	r.stopContainersMarkedAsStopped(project)

	logger.Info("Project %s reconciled successfully (stack: %s)", project, targetProjectName)
	return nil
}

func (r *Reconciler) cleanupConflictingContainers(project string) error {
	// Find all containers (including non-managed) that might conflict
	// This is safe because we only remove containers with names defined in the compose file
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")

	// Parse compose file to get container names
	containerNames, err := r.getContainerNamesFromCompose(composePath)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Remove each container if it exists
	for _, containerName := range containerNames {
		// Check if container exists
		checkCmd := exec.Command("docker", "ps", "-aq", "--filter", fmt.Sprintf("name=^%s$", containerName))
		output, err := checkCmd.Output()
		if err != nil || len(output) == 0 {
			continue // Container doesn't exist, skip
		}

		containerID := strings.TrimSpace(string(output))
		logger.Info("Removing conflicting container: %s (%s)", containerName, containerID)

		removeCmd := exec.Command("docker", "rm", "-f", containerID)
		if err := removeCmd.Run(); err != nil {
			logger.Warn("Failed to remove container %s: %v", containerName, err)
		}
	}

	return nil
}

func (r *Reconciler) getContainerNamesFromCompose(composePath string) ([]string, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, err
	}

	// Simple YAML parsing to find container_name fields
	// This is a basic implementation - could be improved with proper YAML parsing
	var names []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "container_name:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				name = strings.Trim(name, `"'`)
				names = append(names, name)
			}
		}
	}

	return names, nil
}

// downProject removes an orphan project entirely (app was deleted from apps/).
// Always performs full cleanup: containers, networks, volumes, and images.
// konta.stopped label is irrelevant here — the app is gone from configuration.
func (r *Reconciler) downProject(project string) error {
	stacks, err := r.listStacksForApp(project)
	if err != nil {
		return fmt.Errorf("failed to list stacks for orphan project %s: %w", project, err)
	}

	if len(stacks) == 0 {
		stacks = []string{project}
	}

	for _, stack := range stacks {
		logger.Info("Removing orphan stack %s for app %s (full cleanup: containers, networks, volumes, images)", stack, project)
		if err := r.downComposeProject(stack, true); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reconciler) handleProjectModeMigration(baseProject string, targetProjectName string, rollingEnabled bool) error {
	composePath := filepath.Join(r.appsDir, baseProject, "docker-compose.yml")
	workDir := filepath.Join(r.appsDir, baseProject)

	stacks, err := r.listStacksForApp(baseProject)
	if err != nil {
		return fmt.Errorf("failed to list existing stacks for project %s: %w", baseProject, err)
	}

	hasLegacy := false
	hasHashed := false
	for _, stack := range stacks {
		if stack == baseProject {
			hasLegacy = true
		} else if strings.HasPrefix(stack, baseProject+"-") {
			hasHashed = true
		}
	}

	if rollingEnabled && hasLegacy && baseProject != targetProjectName {
		logger.Info("Migrating project %s from non-rolling to rolling mode via restart", baseProject)
		if r.dryRun {
			return nil
		}
		if err := r.downComposeProjectWithContext(baseProject, composePath, workDir, false); err != nil {
			return fmt.Errorf("failed migration down for project %s: %w", baseProject, err)
		}
	}

	if !rollingEnabled && hasHashed {
		logger.Info("Migrating project %s from rolling to non-rolling mode via restart", baseProject)
		if r.dryRun {
			return nil
		}
		for _, stack := range stacks {
			if stack != baseProject {
				if err := r.downComposeProjectWithContext(stack, composePath, workDir, false); err != nil {
					return fmt.Errorf("failed to stop rolling stack %s during migration: %w", stack, err)
				}
			}
		}
	}

	return nil
}

func (r *Reconciler) cleanupOldStacksForApp(baseProject string, keepStack string, composePath string, workDir string) error {
	stacks, err := r.listStacksForApp(baseProject)
	if err != nil {
		return err
	}

	for _, stack := range stacks {
		if stack == keepStack {
			continue
		}
		if err := r.downComposeProjectWithContext(stack, composePath, workDir, true); err != nil {
			return fmt.Errorf("failed to cleanup old stack %s: %w", stack, err)
		}
	}

	return nil
}

func (r *Reconciler) listStacksForApp(baseProject string) ([]string, error) {
	cmd := exec.Command("docker", "ps", "-a", "--filter", "label=konta.managed=true", "--format", "{{.Label \"konta.app\"}}|{{.Label \"com.docker.compose.project\"}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	stacks := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		appName := strings.TrimSpace(parts[0])
		composeProject := strings.TrimSpace(parts[1])
		if composeProject == "" {
			continue
		}

		if appName == baseProject || composeProject == baseProject || strings.HasPrefix(composeProject, baseProject+"-") {
			if !seen[composeProject] {
				seen[composeProject] = true
				stacks = append(stacks, composeProject)
			}
		}
	}

	sort.Strings(stacks)
	return stacks, nil
}

func (r *Reconciler) hasStack(baseProject string, stackName string) (bool, error) {
	stacks, err := r.listStacksForApp(baseProject)
	if err != nil {
		return false, err
	}

	for _, stack := range stacks {
		if stack == stackName {
			return true, nil
		}
	}

	return false, nil
}

func (r *Reconciler) downComposeProject(projectName string, fullCleanup bool) error {
	return r.downComposeProjectWithContext(projectName, "", "", fullCleanup)
}

func (r *Reconciler) downComposeProjectWithContext(projectName string, composePath string, workDir string, fullCleanup bool) error {
	args := []string{"compose", "-p", projectName, "down", "--remove-orphans"}
	if strings.TrimSpace(composePath) != "" {
		args = append(args, "-f", composePath)
	}
	if fullCleanup {
		args = append(args, "--volumes", "--rmi", "all")
	}

	cmd := exec.Command("docker", args...)
	if strings.TrimSpace(workDir) != "" {
		cmd.Dir = workDir
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down failed for %s: %w", projectName, err)
	}

	return nil
}

func (r *Reconciler) composeHasLabel(composePath string, label string) (bool, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return false, err
	}
	content := strings.ToLower(string(data))
	return strings.Contains(content, strings.ToLower(label)), nil
}

func (r *Reconciler) composeHasHealthcheck(composePath string) (bool, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return false, err
	}
	content := strings.ToLower(string(data))
	return strings.Contains(content, "healthcheck:"), nil
}

func (r *Reconciler) waitForProjectHealthy(projectName string, timeoutSeconds int) error {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}

	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", projectName), "--format", "{{.State}}|{{.Status}}")
		output, err := cmd.Output()
		if err != nil {
			return err
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		allHealthy := true
		hasContainers := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			hasContainers = true
			parts := strings.SplitN(line, "|", 2)
			if len(parts) != 2 {
				allHealthy = false
				break
			}
			state := strings.TrimSpace(parts[0])
			status := strings.ToLower(strings.TrimSpace(parts[1]))
			if state != "running" || !strings.Contains(status, "(healthy)") {
				allHealthy = false
				break
			}
		}

		if hasContainers && allHealthy {
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timed out waiting for healthy containers in stack %s", projectName)
}

func (r *Reconciler) waitForProjectHealthyWithRetries(projectName string, timeoutSeconds int, retries int) error {
	if retries <= 0 {
		retries = 1
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			logger.Warn("Retrying health check for stack %s (%d/%d)", projectName, attempt, retries)
		}

		if err := r.waitForProjectHealthy(projectName, timeoutSeconds); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("all %d healthcheck attempt(s) failed: %w", retries, lastErr)
	}

	return fmt.Errorf("all %d healthcheck attempt(s) failed", retries)
}

func (r *Reconciler) waitForProjectRunning(projectName string, timeoutSeconds int) error {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}

	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", projectName), "--format", "{{.State}}")
		output, err := cmd.Output()
		if err != nil {
			return err
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		allRunning := true
		hasContainers := false
		for _, line := range lines {
			state := strings.TrimSpace(line)
			if state == "" {
				continue
			}
			hasContainers = true
			if state != "running" {
				allRunning = false
				break
			}
		}

		if hasContainers && allRunning {
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timed out waiting for running containers in stack %s", projectName)
}

func (r *Reconciler) waitForProjectRunningWithRetries(projectName string, timeoutSeconds int, retries int) error {
	if retries <= 0 {
		retries = 1
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			logger.Warn("Retrying runtime check for stack %s (%d/%d)", projectName, attempt, retries)
		}

		if err := r.waitForProjectRunning(projectName, timeoutSeconds); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("all %d runtime check attempt(s) failed: %w", retries, lastErr)
	}

	return fmt.Errorf("all %d runtime check attempt(s) failed", retries)
}

func (r *Reconciler) shortDeployCommit() string {
	commit := strings.TrimSpace(r.deployCommit)
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func (r *Reconciler) shouldUseHashInProjectName(rollingEnabled bool) bool {
	mode := strings.ToLower(strings.TrimSpace(r.config.Deploy.ProjectNameHashMode))
	switch mode {
	case "all":
		return true
	case "none":
		return false
	default:
		return rollingEnabled
	}
}

// hasAnyContainersForApp returns true when there is at least one container
// (any state) associated with the given base app name.
// It checks both the konta.app label (used for rolling stacks) and the
// compose project name (used for non-rolling stacks or legacy stacks without
// the label). Rolling stacks whose compose project name starts with
// "<project>-" are also matched.
func (r *Reconciler) hasAnyContainersForApp(project string) bool {
	// Check by konta.app label — present on well-labelled rolling stacks
	cmd := exec.Command("docker", "ps", "-a",
		"--filter", "label=konta.managed=true",
		"--filter", fmt.Sprintf("label=konta.app=%s", project),
		"--format", "{{.ID}}",
	)
	if output, err := cmd.Output(); err == nil && strings.TrimSpace(string(output)) != "" {
		return true
	}

	// Check by exact compose project name (non-rolling)
	cmd = exec.Command("docker", "ps", "-a",
		"--filter", "label=konta.managed=true",
		"--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project),
		"--format", "{{.ID}}",
	)
	if output, err := cmd.Output(); err == nil && strings.TrimSpace(string(output)) != "" {
		return true
	}

	// Check rolling stacks whose compose project starts with "<project>-<hash>"
	// by listing all stacks and checking the prefix (reuses listStacksForApp).
	stacks, err := r.listStacksForApp(project)
	if err == nil && len(stacks) > 0 {
		return true
	}

	return false
}

// hasStoppedContainers checks if a project has any stopped containers
// Ignores containers marked with konta.stopped=true
func (r *Reconciler) hasStoppedContainers(project string) (bool, error) {
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")

	// Check if compose file defines any services
	if _, err := os.Stat(composePath); err != nil {
		return false, err
	}

	// First, handle containers marked with konta.stopped=true - stop them if running
	baseFilters := []string{"--filter", "label=konta.managed=true"}
	if r.appHasLabeledStacks(project) {
		baseFilters = append(baseFilters, "--filter", fmt.Sprintf("label=konta.app=%s", project))
	} else {
		baseFilters = append(baseFilters, "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project))
	}

	stopArgs := []string{"ps"}
	stopArgs = append(stopArgs, baseFilters...)
	stopArgs = append(stopArgs,
		"--filter", "label=konta.stopped=true",
		"--filter", "status=running",
		"--format", "{{.ID}}",
	)
	stopCmd := exec.Command("docker", stopArgs...)

	output, err := stopCmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		// Found running containers marked to be stopped
		containers := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, containerID := range containers {
			if containerID != "" {
				logger.Info("Stopping container marked with konta.stopped=true: %s", containerID[:12])
				if !r.dryRun {
					doStopCmd := exec.Command("docker", "stop", containerID)
					if err := doStopCmd.Run(); err != nil {
						logger.Warn("Failed to stop container %s: %v", containerID[:12], err)
					}
				}
			}
		}
	}

	// Check for stopped containers that should be running (excluding konta.stopped=true)
	checkArgs := []string{"ps", "-a"}
	checkArgs = append(checkArgs, baseFilters...)
	checkArgs = append(checkArgs,
		"--filter", "status=exited",
		"--format", "{{.ID}}|{{.Label \"konta.stopped\"}}",
	)
	checkCmd := exec.Command("docker", checkArgs...)

	output, err = checkCmd.Output()
	if err != nil {
		// If command fails, assume no stopped containers
		return false, nil
	}

	// Check if any exited containers don't have konta.stopped=true
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) > 1 && parts[1] != "true" {
			// Found a stopped container that should be running
			return true, nil
		}
	}

	return false, nil
}

func (r *Reconciler) hasUnhealthyContainers(project string) (bool, error) {
	baseFilters := []string{"--filter", "label=konta.managed=true"}
	if r.appHasLabeledStacks(project) {
		baseFilters = append(baseFilters, "--filter", fmt.Sprintf("label=konta.app=%s", project))
	} else {
		baseFilters = append(baseFilters, "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project))
	}

	args := []string{"ps", "-a"}
	args = append(args, baseFilters...)
	args = append(args, "--format", "{{.Status}}|{{.Label \"konta.stopped\"}}")

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}

		status := strings.ToLower(strings.TrimSpace(parts[0]))
		stoppedLabel := strings.TrimSpace(parts[1])
		if stoppedLabel == "true" {
			continue
		}

		if strings.Contains(status, "(unhealthy)") {
			return true, nil
		}
	}

	return false, nil
}

func (r *Reconciler) startProject(project string) error {
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")

	if r.appHasLabeledStacks(project) {
		startCmd := exec.Command(
			"docker", "ps",
			"-a",
			"--filter", "label=konta.managed=true",
			"--filter", fmt.Sprintf("label=konta.app=%s", project),
			"--filter", "status=exited",
			"--format", "{{.ID}}",
		)
		output, err := startCmd.Output()
		if err == nil {
			containerIDs := strings.Fields(string(output))
			if len(containerIDs) > 0 {
				if r.dryRun {
					logger.Info("[DRY-RUN] Would start %d stopped container(s) for rolling app %s", len(containerIDs), project)
					return nil
				}

				cmd := exec.Command("docker", "start")
				cmd.Args = append(cmd.Args, containerIDs...)
				cmd.Stdout = os.Stderr
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err == nil {
					logger.Info("Project %s started successfully", project)
					return nil
				}
			}
		}
	}

	if r.dryRun {
		logger.Info("[DRY-RUN] Would start containers for project %s", project)
		return nil
	}

	cmd := exec.Command(
		"docker", "compose",
		"-p", project,
		"-f", composePath,
		"up", "-d",
		"--remove-orphans",
	)

	cmd.Dir = filepath.Join(r.appsDir, project)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// Ensure konta management labels are set
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_LABELS=konta.managed=true,konta.app=%s,konta.commit=%s", project, r.shortDeployCommit()))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start project %s: %w", project, err)
	}

	logger.Info("Project %s started successfully", project)
	return nil
}

// shouldProjectBeStopped checks if any containers in the project have konta.stopped=true
func (r *Reconciler) shouldProjectBeStopped(project string) (bool, error) {
	// Check if any containers are marked with konta.stopped=true
	cmd := exec.Command(
		"docker", "ps",
		"-a",
		"--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project),
		"--filter", "label=konta.managed=true",
		"--filter", "label=konta.stopped=true",
		"--format", "{{.ID}}",
	)

	output, err := cmd.Output()
	if err != nil {
		return false, nil
	}

	// If we found containers marked to be stopped, project should be stopped
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// stopContainersMarkedAsStopped stops any running containers marked with konta.stopped=true
func (r *Reconciler) stopContainersMarkedAsStopped(project string) {
	args := []string{"ps", "--filter", "label=konta.managed=true"}
	if r.appHasLabeledStacks(project) {
		args = append(args, "--filter", fmt.Sprintf("label=konta.app=%s", project))
	} else {
		args = append(args, "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project))
	}
	args = append(args,
		"--filter", "label=konta.stopped=true",
		"--filter", "status=running",
		"--format", "{{.ID}}",
	)

	stopCmd := exec.Command("docker", args...)

	output, err := stopCmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		// Found running containers marked to be stopped
		containers := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, containerID := range containers {
			if containerID != "" {
				logger.Info("Stopping container marked with konta.stopped=true: %s", containerID[:12])
				if !r.dryRun {
					doStopCmd := exec.Command("docker", "stop", containerID)
					if err := doStopCmd.Run(); err != nil {
						logger.Warn("Failed to stop container %s: %v", containerID[:12], err)
					}
				}
			}
		}
	}
}

func (r *Reconciler) appHasLabeledStacks(project string) bool {
	cmd := exec.Command(
		"docker", "ps", "-a",
		"--filter", "label=konta.managed=true",
		"--filter", fmt.Sprintf("label=konta.app=%s", project),
		"--format", "{{.ID}}",
	)

	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(output)) != ""
}

// stopProject stops all containers for a project
func (r *Reconciler) stopProject(project string) error {
	if r.dryRun {
		logger.Info("[DRY-RUN] Would stop containers for project %s", project)
		return nil
	}

	cmd := exec.Command(
		"docker", "compose",
		"-p", project,
		"stop",
	)

	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop project %s: %w", project, err)
	}

	logger.Info("Project %s stopped successfully", project)
	return nil
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func sameStringSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *Reconciler) hasDeploymentDrift(project string) (bool, string, error) {
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")
	rollingEnabled, err := r.composeHasLabel(composePath, "konta.rolling=true")
	if err != nil {
		return false, "", err
	}

	useHashName := r.shouldUseHashInProjectName(rollingEnabled)
	expectedStack := project
	if useHashName {
		shortCommit := r.shortDeployCommit()
		if shortCommit == "" {
			return false, "", fmt.Errorf("deploy commit is required when hash-based project naming is enabled")
		}
		expectedStack = fmt.Sprintf("%s-%s", project, shortCommit)
	}

	stacks, err := r.listStacksForApp(project)
	if err != nil {
		return false, "", err
	}

	if !contains(stacks, expectedStack) {
		return true, fmt.Sprintf("expected stack %s is missing (found: %v)", expectedStack, stacks), nil
	}

	if len(stacks) > 1 {
		return true, fmt.Sprintf("multiple stacks detected for app (expected only %s, found: %v)", expectedStack, stacks), nil
	}

	expectedServices, err := r.getExpectedServicesForStack(project, expectedStack, composePath)
	if err != nil {
		return false, "", err
	}

	runningServices, err := r.getRunningManagedServicesForStack(expectedStack)
	if err != nil {
		return false, "", err
	}

	if !sameStringSet(expectedServices, runningServices) {
		return true, fmt.Sprintf("service set mismatch (expected: %v, running: %v)", expectedServices, runningServices), nil
	}

	return false, "", nil
}

func (r *Reconciler) getExpectedServicesForStack(project string, stackName string, composePath string) ([]string, error) {
	cmd := exec.Command(
		"docker", "compose",
		"-p", stackName,
		"-f", composePath,
		"config", "--services",
	)
	cmd.Dir = filepath.Join(r.appsDir, project)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve compose services for stack %s: %w", stackName, err)
	}

	services := strings.Fields(string(output))
	return uniqueStrings(services), nil
}

func (r *Reconciler) getRunningManagedServicesForStack(stackName string) ([]string, error) {
	cmd := exec.Command(
		"docker", "ps", "-a",
		"--filter", "label=konta.managed=true",
		"--filter", fmt.Sprintf("label=com.docker.compose.project=%s", stackName),
		"--format", "{{.Label \"com.docker.compose.service\"}}",
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	services := strings.Split(strings.TrimSpace(string(output)), "\n")
	return uniqueStrings(services), nil
}

func isProjectPresentInRunning(project string, running []string) bool {
	if contains(running, project) {
		return true
	}

	prefix := project + "-"
	for _, r := range running {
		if strings.HasPrefix(r, prefix) {
			suffix := r[len(prefix):]
			if isShortCommitHash(suffix) {
				return true
			}
		}
	}

	return false
}

// isShortCommitHash returns true when s looks like an 8-character lowercase hex string
// (the short-commit suffix used in rolling stack names).
func isShortCommitHash(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// isDesiredOrRollingStack returns true when the running project name either
// matches a desired project directly, or is a rolling stack for one
// (pattern: <desiredApp>-<8hexchars>).
// Rolling stacks whose base app is still desired are managed by
// cleanupOldStacksForApp and must NOT be treated as orphans here.
func isDesiredOrRollingStack(project string, desired []string) bool {
	if contains(desired, project) {
		return true
	}
	for _, d := range desired {
		prefix := d + "-"
		if strings.HasPrefix(project, prefix) {
			suffix := project[len(prefix):]
			if isShortCommitHash(suffix) {
				return true
			}
		}
	}
	return false
}
