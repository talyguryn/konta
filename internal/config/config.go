package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/types"
)

var (
	configPaths = []string{
		"/etc/konta/config.yaml",
		filepath.Join(os.Getenv("HOME"), ".konta", "config.yaml"),
		"./konta.yaml",
		"./gitops.yaml", // Backward compatibility
	}
)

// FindConfigPath returns the first existing config path.
func FindConfigPath() (string, error) {
	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no configuration file found. Checked: %v", configPaths)
}

func Load() (*types.Config, error) {
	configPath, err := FindConfigPath()
	if err != nil {
		return nil, err
	}

	logger.Debug("Loading config from: %s", configPath)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &types.Config{
		Repository: types.RepositoryConf{
			Path:     ".",
			Interval: 120,
			Branch:   "main",
		},
		Deploy: types.DeployConf{
			Atomic: true,
		},
		Logging: types.LoggingConf{
			Level: "info",
		},
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate config
	if config.Version == "" {
		config.Version = "v1"
	}
	if config.Repository.URL == "" {
		return nil, fmt.Errorf("repository.url is required")
	}

	// Normalize repository path - ensure it points to 'apps' directory
	// If path ends with 'apps', keep it
	// Otherwise, append 'apps' to the path
	if config.Repository.Path == "" || config.Repository.Path == "." {
		config.Repository.Path = "apps"
	} else if !strings.HasSuffix(config.Repository.Path, "apps") {
		config.Repository.Path = filepath.Join(config.Repository.Path, "apps")
	}

	// Get the base directory (parent of apps directory) for hooks location
	appsBasePath := filepath.Dir(config.Repository.Path)
	if appsBasePath == "." {
		appsBasePath = ""
	}

	// Configure hooks with default paths if not explicitly set
	// Hooks now accept just the filename (e.g., "pre.sh", "post_update.sh")
	// They are resolved relative to {repo_root}/hooks/ directory
	hooksBase := filepath.Join(appsBasePath, "hooks")

	if config.Hooks.Started == "" {
		config.Hooks.Started = "started.sh"
	}
	if config.Hooks.Pre == "" {
		config.Hooks.Pre = "pre.sh"
	}
	if config.Hooks.Success == "" {
		config.Hooks.Success = "success.sh"
	}
	if config.Hooks.Failure == "" {
		config.Hooks.Failure = "failure.sh"
	}
	if config.Hooks.PostUpdate == "" {
		config.Hooks.PostUpdate = "post_update.sh"
	}

	// Build absolute paths (relative to repo root, will be resolved later)
	config.Hooks.StartedAbs = filepath.Join(hooksBase, config.Hooks.Started)
	config.Hooks.PreAbs = filepath.Join(hooksBase, config.Hooks.Pre)
	config.Hooks.SuccessAbs = filepath.Join(hooksBase, config.Hooks.Success)
	config.Hooks.FailureAbs = filepath.Join(hooksBase, config.Hooks.Failure)
	config.Hooks.PostUpdateAbs = filepath.Join(hooksBase, config.Hooks.PostUpdate)

	// Override token from environment if set
	if token := os.Getenv("KONTA_TOKEN"); token != "" {
		config.Repository.Token = token
	}

	// Validate config and save lock file
	if err := validateAndLockConfig(config, configPath); err != nil {
		return nil, err
	}

	return config, nil
}

// validateAndLockConfig validates the config and creates a lock file with full config backup
func validateAndLockConfig(config *types.Config, configPath string) error {
	lockPath := configPath + ".lock"

	// Create lock file with full config for recovery and change detection
	lockData := map[string]interface{}{
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"config":    config,
	}

	lockBytes, _ := yaml.Marshal(lockData)
	if err := os.WriteFile(lockPath, lockBytes, 0644); err != nil {
		logger.Warn("Failed to write config lock file: %v", err)
		// Don't fail on lock file error, just warn
	}

	return nil
}

// HasConfigChanged checks if the current config differs from the locked version
func HasConfigChanged(config *types.Config, configPath string) bool {
	lockPath := configPath + ".lock"

	// Read lock file
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		// Lock file doesn't exist, consider it changed
		return true
	}

	var lock map[string]interface{}
	if err := yaml.Unmarshal(lockData, &lock); err != nil {
		logger.Debug("Failed to parse lock file: %v", err)
		return true
	}

	// Extract config from lock file and compare
	if lockedCfgInterface, ok := lock["config"]; ok {
		// Re-marshal both configs to compare their YAML representation
		currentData, _ := yaml.Marshal(config)
		lockedData, _ := yaml.Marshal(lockedCfgInterface)

		hasChanged := string(currentData) != string(lockedData)
		if hasChanged {
			logger.Info("Config file has been modified since last load")
		}
		return hasChanged
	}

	return true
}

// Save saves the configuration to the default location
func Save(config *types.Config) error {
	configPath := "/etc/konta/config.yaml"

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	logger.Info("Config saved to: %s", configPath)
	return nil
}
