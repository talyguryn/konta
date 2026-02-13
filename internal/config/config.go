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

func Load() (*types.Config, error) {
	configPath := ""

	// Find the first existing config file
	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			configPath = path
			break
		}
	}

	if configPath == "" {
		return nil, fmt.Errorf("no configuration file found. Checked: %v", configPaths)
	}

	logger.Info("Loading config from: %s", configPath)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &types.Config{
		Repository: types.RepositoryConf{
			Path:     "apps",
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

	// Configure hooks with default paths if not explicitly set
	configDir := filepath.Dir(configPath)
	if config.Hooks.Pre == "" {
		config.Hooks.Pre = filepath.Join(config.Repository.Path, "hooks", "pre.sh")
	}
	if config.Hooks.Success == "" {
		config.Hooks.Success = filepath.Join(config.Repository.Path, "hooks", "success.sh")
	}
	if config.Hooks.Failure == "" {
		config.Hooks.Failure = filepath.Join(config.Repository.Path, "hooks", "failure.sh")
	}

	// Convert relative paths to absolute (relative to repo root, not config dir)
	if !filepath.IsAbs(config.Hooks.Pre) && !strings.HasPrefix(config.Hooks.Pre, "/") {
		config.Hooks.PreAbs = filepath.Join(configDir, config.Hooks.Pre)
	} else {
		config.Hooks.PreAbs = config.Hooks.Pre
	}
	if !filepath.IsAbs(config.Hooks.Success) && !strings.HasPrefix(config.Hooks.Success, "/") {
		config.Hooks.SuccessAbs = filepath.Join(configDir, config.Hooks.Success)
	} else {
		config.Hooks.SuccessAbs = config.Hooks.Success
	}
	if !filepath.IsAbs(config.Hooks.Failure) && !strings.HasPrefix(config.Hooks.Failure, "/") {
		config.Hooks.FailureAbs = filepath.Join(configDir, config.Hooks.Failure)
	} else {
		config.Hooks.FailureAbs = config.Hooks.Failure
	}

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
