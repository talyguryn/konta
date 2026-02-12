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
	config    *types.Config
	repoDir   string
	dryRun    bool
	appsDir   string
}

// New creates a new reconciler
func New(config *types.Config, repoDir string, dryRun bool) *Reconciler {
	return &Reconciler{
		config:  config,
		repoDir: repoDir,
		dryRun:  dryRun,
		appsDir: filepath.Join(repoDir, config.Repository.Path),
	}
}

// Reconcile performs the reconciliation
func (r *Reconciler) Reconcile() error {
	logger.Info("Starting reconciliation")

	// Get desired projects from git
	desired, err := r.getDesiredProjects()
	if err != nil {
		return fmt.Errorf("failed to get desired projects: %w", err)
	}

	logger.Info("Found %d desired projects", len(desired))

	// Get currently running projects
	running, err := r.getRunningProjects()
	if err != nil {
		return fmt.Errorf("failed to get running projects: %w", err)
	}

	logger.Info("Found %d running projects", len(running))

	// Reconcile desired projects
	for _, project := range desired {
		if err := r.reconcileProject(project); err != nil {
			return fmt.Errorf("failed to reconcile project %s: %w", project, err)
		}
	}

	// Remove orphan projects (only Konta-managed ones)
	// Since getRunningProjects() already filters by konta.managed=true,
	// we're only removing containers that Konta created but are no longer in Git
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

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}