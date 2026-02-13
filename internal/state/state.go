package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	currentState.Version = "0.1.0"

	// Update per-project state for reconciled projects
	deployTime := time.Now().Format("2006-01-02 15:04:05")
	for _, project := range reconciledProjects {
		currentState.Projects[project] = types.ProjectState{
			LastCommit:     commit,
			LastDeployTime: deployTime,
		}
	}

	if err := Save(currentState); err != nil {
		return err
	}

	logger.Info("State updated: commit=%s", commit)
	return nil
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
