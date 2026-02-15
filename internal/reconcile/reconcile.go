package reconcile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/types"
)

// Reconciler manages the reconciliation process
type Reconciler struct {
	config         *types.Config
	repoDir        string
	dryRun         bool
	appsDir        string
	changedProjects map[string]bool // Track which projects have changes
}

// New creates a new reconciler
func New(config *types.Config, repoDir string, dryRun bool) *Reconciler {
	return &Reconciler{
		config:         config,
		repoDir:        repoDir,
		dryRun:         dryRun,
		appsDir:        filepath.Join(repoDir, config.Repository.Path),
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
// Returns the list of projects that were actually reconciled
func (r *Reconciler) Reconcile() ([]string, error) {
	logger.Info("Starting reconciliation")

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

	// Track which projects were reconciled
	reconciledProjects := []string{}

	// Reconcile desired projects
	for _, project := range desired {
		// Skip projects that haven't changed (unless changedProjects is nil, meaning reconcile all)
		if r.changedProjects != nil && !r.changedProjects[project] {
			logger.Info("Skipping project %s (no changes detected)", project)
			continue
		}

		if err := r.reconcileProject(project); err != nil {
			return reconciledProjects, fmt.Errorf("failed to reconcile project %s: %w", project, err)
		}
		reconciledProjects = append(reconciledProjects, project)
	}

	// Ensure all desired projects have their containers running
	// This handles cases where containers were stopped but config didn't change
	for _, project := range desired {
		// Skip if we already reconciled this project
		if contains(reconciledProjects, project) {
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
			}
		}
	}

	// Remove orphan projects (only Konta-managed ones with konta.managed=true label)
	// We only remove projects that Konta explicitly manages, never other projects
	for _, project := range running {
		if !contains(desired, project) {
			logger.Info("Removing orphan Konta-managed project: %s", project)
			if !r.dryRun {
				if err := r.downProject(project); err != nil {
					logger.Error("Failed to remove project %s: %v", project, err)
				}
			} else {
				logger.Info("[DRY-RUN] Would remove project: %s", project)
			}
		}
	}

	logger.Info("Reconciliation complete")
	return reconciledProjects, nil
}

// HealthCheck ensures all desired containers are running (used when no code changes detected)
func (r *Reconciler) HealthCheck() ([]string, error) {
	logger.Info("Starting container health check")

	// Get desired projects from git
	desired, err := r.getDesiredProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get desired projects: %w", err)
	}

	logger.Debug("Checking health of %d desired projects", len(desired))

	// Track which projects were started
	startedProjects := []string{}

	// Check if all desired projects have their containers running
	for _, project := range desired {
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
				startedProjects = append(startedProjects, project)
			}
		}
	}

	// Remove orphan projects (only Konta-managed ones with konta.managed=true label)
	// This ensures orphans are cleaned up even when no code changes are detected
	running, err := r.getRunningProjects()
	if err != nil {
		logger.Warn("Failed to get running projects: %v", err)
	} else {
		for _, project := range running {
			if !contains(desired, project) {
				logger.Info("Removing orphan Konta-managed project: %s", project)
				if !r.dryRun {
					if err := r.downProject(project); err != nil {
						logger.Error("Failed to remove project %s: %v", project, err)
					}
				} else {
					logger.Info("[DRY-RUN] Would remove project: %s", project)
				}
			}
		}
	}

	logger.Info("Health check complete")
	return startedProjects, nil
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

	// Remove orphan projects (only Konta-managed ones)
	for _, project := range running {
		if !contains(desired, project) {
			logger.Info("Removing orphan Konta-managed project: %s", project)
			if !r.dryRun {
				if err := r.downProject(project); err != nil {
					logger.Error("Failed to remove project %s: %v", project, err)
				}
			} else {
				logger.Info("[DRY-RUN] Would remove project: %s", project)
			}
		}
	}

	logger.Info("Orphan cleanup complete")
	return nil
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
	// Only get projects managed by Konta (with konta.managed=true label)
	cmd := exec.Command("docker", "ps", "--filter", "label=konta.managed=true", "--format", "{{.Label \"com.docker.compose.project\"}}")
	output, err := cmd.Output()
	if err != nil {
		logger.Warn("Failed to get running projects: %v", err)
		return []string{}, nil
	}

	projects := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			projects = append(projects, line)
		}
	}

	sort.Strings(projects)
	return projects, nil
}



func (r *Reconciler) reconcileProject(project string) error {
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")

	logger.Info("Reconciling project: %s", project)

	if r.dryRun {
		logger.Info("[DRY-RUN] Would run docker compose for %s", project)
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
	var stderr bytes.Buffer
	cmd.Stdout = os.Stderr
	cmd.Stderr = &stderr
	// Add Konta management label to all containers in this project
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_LABELS=konta.managed=true")

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
				"-p", project,
				"-f", composePath,
				"up", "-d",
				"--remove-orphans",
			)
			cmd.Dir = filepath.Join(r.appsDir, project)
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_LABELS=konta.managed=true")

			if retryErr := cmd.Run(); retryErr != nil {
				return fmt.Errorf("docker compose failed after cleanup retry: %w (original: %v)", retryErr, stderrStr)
			}

			logger.Info("Successfully resolved container name conflict")
		} else {
			// Not a conflict error, return original error with stderr
			return fmt.Errorf("docker compose failed: %w\nStderr: %s", err, stderrStr)
		}
	}

	// After successful compose up, immediately stop containers marked with konta.stopped=true
	r.stopContainersMarkedAsStopped(project)

	logger.Info("Project %s reconciled successfully", project)
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

func (r *Reconciler) downProject(project string) error {
	cmd := exec.Command(
		"docker", "compose",
		"-p", project,
		"down",
		"--remove-orphans",
	)

	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down failed: %w", err)
	}

	return nil
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
	stopCmd := exec.Command(
		"docker", "ps",
		"--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project),
		"--filter", "label=konta.managed=true",
		"--filter", "label=konta.stopped=true",
		"--filter", "status=running",
		"--format", "{{.ID}}",
	)

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
	checkCmd := exec.Command(
		"docker", "ps",
		"-a",
		"--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project),
		"--filter", "label=konta.managed=true",
		"--filter", "status=exited",
		"--format", "{{.ID}}|{{.Label \"konta.stopped\"}}",
	)

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

func (r *Reconciler) startProject(project string) error {
	composePath := filepath.Join(r.appsDir, project, "docker-compose.yml")

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
	// Ensure konta management label is set
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_LABELS=konta.managed=true")

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
	stopCmd := exec.Command(
		"docker", "ps",
		"--filter", fmt.Sprintf("label=com.docker.compose.project=%s", project),
		"--filter", "label=konta.managed=true",
		"--filter", "label=konta.stopped=true",
		"--filter", "status=running",
		"--format", "{{.ID}}",
	)

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