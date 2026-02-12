package config

import (
	"fmt"
	"os"
	"path/filepath"

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

// Load loads the configuration
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

	// Override token from environment if set
	if token := os.Getenv("KONTA_TOKEN"); token != "" {
		config.Repository.Token = token
	}

	return config, nil
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
