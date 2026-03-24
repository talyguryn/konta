package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/types"
)

var (
	stateDir string
)

// getStateDir returns the state directory, creating fallback path if needed
func getStateDir() string {
	if stateDir != "" {
		return stateDir
	}

	// Try to use /var/lib/konta first
	primaryPath := "/var/lib/konta"
	primaryParent := "/var/lib"
	if _, err := os.Stat(primaryParent); err == nil {
		// /var/lib exists, check if we can write to it
		testFile := filepath.Join(primaryParent, ".konta_test")
		if f, err := os.Create(testFile); err == nil {
			_ = f.Close()
			_ = os.Remove(testFile)
			stateDir = primaryPath
			return stateDir
		}
	}

	// Fallback to home directory
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = "/tmp"
	}
	stateDir = filepath.Join(homeDir, ".konta", "state")
	return stateDir
}

// Init initializes the state directory
func Init() error {
	dir := getStateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	return nil
}

// Load loads the state
func Load() (*types.State, error) {
	path := filepath.Join(getStateDir(), "state.json")
	if _, err := os.Stat(path); err != nil {
		return &types.State{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	state := &types.State{}
	if err := json.Unmarshal(data, state); err != nil {
		logger.Warn("Failed to parse state file: %v", err)
		return &types.State{}, nil
	}

	return state, nil
}

// Save saves the state
func Save(state *types.State) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}

	if state.Version == "" {
		state.Version = "0.1.0"
	}

	path := filepath.Join(getStateDir(), "state.json")

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// Update updates the state after successful deployment
func Update(commit string) error {
	return UpdateWithProjects(commit, nil)
}

// UpdateWithProjects updates the state after successful deployment with per-project tracking
func UpdateWithProjects(commit string, reconciledProjects []string) error {
	// Load existing state to preserve project states
	currentState, err := Load()
	if err != nil {
		logger.Warn("Failed to load existing state: %v", err)
		currentState = &types.State{}
	}

	// Initialize projects map if nil
	if currentState.Projects == nil {
		currentState.Projects = make(map[string]types.ProjectState)
	}

	// Update the global state
	currentState.LastCommit = commit
	currentState.LastDeployTime = time.Now().Format("2006-01-02 15:04:05")
	currentState.LastAttemptedCommit = commit
	currentState.LastAttemptStatus = "success"
	currentState.LastAttemptTime = time.Now().Format("2006-01-02 15:04:05")
	currentState.Version = "0.1.0"

	// Update per-project state for reconciled projects
	deployTime := time.Now().Format("2006-01-02 15:04:05")
	for _, project := range reconciledProjects {
		projectState := currentState.Projects[project]
		projectState.LastCommit = commit
		projectState.LastDeployTime = deployTime
		projectState.SelfHealAttempts = 0
		currentState.Projects[project] = projectState
	}

	if err := Save(currentState); err != nil {
		return err
	}

	logger.Info("State updated: commit=%s", commit)
	return nil
}

// PruneProjects removes project state entries that are no longer present in desired apps.
func PruneProjects(desiredProjects []string) error {
	currentState, err := Load()
	if err != nil {
		return err
	}

	if len(currentState.Projects) == 0 {
		return nil
	}

	allowed := make(map[string]bool, len(desiredProjects))
	for _, project := range desiredProjects {
		allowed[project] = true
	}

	removed := 0
	for project := range currentState.Projects {
		if !allowed[project] {
			delete(currentState.Projects, project)
			removed++
		}
	}

	if removed == 0 {
		return nil
	}

	if err := Save(currentState); err != nil {
		return err
	}

	logger.Info("Pruned %d stale project state entrie(s)", removed)
	return nil
}

// MarkAttempt stores information about the latest deployment attempt.
func MarkAttempt(commit string, status string) error {
	currentState, err := Load()
	if err != nil {
		logger.Warn("Failed to load existing state: %v", err)
		currentState = &types.State{}
	}

	currentState.LastAttemptedCommit = commit
	currentState.LastAttemptStatus = status
	currentState.LastAttemptTime = time.Now().Format("2006-01-02 15:04:05")

	if err := Save(currentState); err != nil {
		return err
	}

	logger.Debug("State attempt updated: commit=%s status=%s", commit, status)
	return nil
}

// GetProjectSelfHealAttempts returns current self-heal attempt count for project.
func GetProjectSelfHealAttempts(project string) (int, error) {
	currentState, err := Load()
	if err != nil {
		return 0, err
	}

	if currentState.Projects == nil {
		return 0, nil
	}

	projectState, ok := currentState.Projects[project]
	if !ok {
		return 0, nil
	}

	if projectState.SelfHealAttempts < 0 {
		return 0, nil
	}

	return projectState.SelfHealAttempts, nil
}

// IncrementProjectSelfHealAttempts increments self-heal attempt count for project.
func IncrementProjectSelfHealAttempts(project string) (int, error) {
	currentState, err := Load()
	if err != nil {
		return 0, err
	}

	if currentState.Projects == nil {
		currentState.Projects = make(map[string]types.ProjectState)
	}

	projectState := currentState.Projects[project]
	if projectState.SelfHealAttempts < 0 {
		projectState.SelfHealAttempts = 0
	}
	projectState.SelfHealAttempts++
	currentState.Projects[project] = projectState

	if err := Save(currentState); err != nil {
		return 0, err
	}

	return projectState.SelfHealAttempts, nil
}

// GetProjectLastCommit returns the last known deployed commit for a project.
func GetProjectLastCommit(project string) (string, error) {
	currentState, err := Load()
	if err != nil {
		return "", err
	}

	if currentState.Projects == nil {
		return "", nil
	}

	projectState, ok := currentState.Projects[project]
	if !ok {
		return "", nil
	}

	commit := strings.TrimSpace(projectState.LastCommit)
	if commit != "" {
		return commit, nil
	}

	return strings.TrimSpace(projectState.ActiveCommit), nil
}

// ResetProjectSelfHealAttempts clears self-heal attempts counter for a project.
// The zero value is omitted from state.json due omitempty.
func ResetProjectSelfHealAttempts(project string) error {
	currentState, err := Load()
	if err != nil {
		return err
	}

	if currentState.Projects == nil {
		return nil
	}

	projectState, ok := currentState.Projects[project]
	if !ok {
		return nil
	}

	if projectState.SelfHealAttempts == 0 {
		return nil
	}

	projectState.SelfHealAttempts = 0
	currentState.Projects[project] = projectState

	return Save(currentState)
}

// SetProjectLastCommit updates the deployed commit marker for a project.
func SetProjectLastCommit(project string, commit string) error {
	commit = strings.TrimSpace(commit)
	if project == "" || commit == "" {
		return nil
	}

	currentState, err := Load()
	if err != nil {
		return err
	}

	if currentState.Projects == nil {
		currentState.Projects = make(map[string]types.ProjectState)
	}

	projectState := currentState.Projects[project]
	projectState.LastCommit = commit
	projectState.LastDeployTime = time.Now().Format("2006-01-02 15:04:05")
	currentState.Projects[project] = projectState

	return Save(currentState)
}

// GetStateDir returns the state directory
func GetStateDir() string {
	return getStateDir()
}

// GetReleasesDir returns the releases directory
func GetReleasesDir() string {
	return filepath.Join(getStateDir(), "releases")
}

// GetCurrentLink returns the path to the current symlink
func GetCurrentLink() string {
	return filepath.Join(getStateDir(), "current")
}

// GetCurrentReleaseCommit returns the commit hash of the currently active release
// by resolving the current symlink target under releases/<commit>.
func GetCurrentReleaseCommit() (string, error) {
	currentLink := GetCurrentLink()
	resolvedPath, err := filepath.EvalSymlinks(currentLink)
	if err != nil {
		return "", fmt.Errorf("failed to resolve current symlink: %w", err)
	}

	commit := filepath.Base(resolvedPath)
	if commit == "" || commit == "." || commit == string(filepath.Separator) {
		return "", fmt.Errorf("invalid current release path: %s", resolvedPath)
	}

	return commit, nil
}

// AddManagedExternalNetworks registers networks that were auto-created by Konta.
func AddManagedExternalNetworks(networks []string) error {
	if len(networks) == 0 {
		return nil
	}

	currentState, err := Load()
	if err != nil {
		return err
	}

	merged := append([]string{}, currentState.ManagedExternalNets...)
	merged = append(merged, networks...)
	currentState.ManagedExternalNets = uniqueSortedStrings(merged)

	return Save(currentState)
}

// RemoveManagedExternalNetwork removes a network from Konta-managed network registry.
func RemoveManagedExternalNetwork(network string) error {
	network = strings.TrimSpace(network)
	if network == "" {
		return nil
	}

	currentState, err := Load()
	if err != nil {
		return err
	}

	if len(currentState.ManagedExternalNets) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(currentState.ManagedExternalNets))
	for _, n := range currentState.ManagedExternalNets {
		if strings.TrimSpace(n) == network {
			continue
		}
		filtered = append(filtered, n)
	}

	currentState.ManagedExternalNets = uniqueSortedStrings(filtered)
	return Save(currentState)
}

// ListManagedExternalNetworks returns networks previously auto-created by Konta.
func ListManagedExternalNetworks() ([]string, error) {
	currentState, err := Load()
	if err != nil {
		return nil, err
	}

	return uniqueSortedStrings(currentState.ManagedExternalNets), nil
}

func uniqueSortedStrings(items []string) []string {
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
