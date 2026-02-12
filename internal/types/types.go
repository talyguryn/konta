package types

// Config represents the konta configuration
type Config struct {
	Version    string         `yaml:"version"`
	Repository RepositoryConf `yaml:"repository"`
	Deploy     DeployConf     `yaml:"deploy,omitempty"`
	Hooks      HooksConf      `yaml:"hooks,omitempty"`
	Logging    LoggingConf    `yaml:"logging,omitempty"`
}

// RepositoryConf represents git repository configuration
type RepositoryConf struct {
	URL      string `yaml:"url"`
	Branch   string `yaml:"branch"`
	Token    string `yaml:"token"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"` // seconds
}

// DeployConf represents deployment configuration
type DeployConf struct {
	Atomic         bool `yaml:"atomic,omitempty"`
	Parallel       bool `yaml:"parallel,omitempty"`
	DryRun         bool `yaml:"dry_run,omitempty"`
}

// HooksConf represents hooks configuration
type HooksConf struct {
	Pre     string `yaml:"pre,omitempty"`
	Success string `yaml:"success,omitempty"`
	Failure string `yaml:"failure,omitempty"`
}

// LoggingConf represents logging configuration
type LoggingConf struct {
	Level  string `yaml:"level,omitempty"` // debug, info, warn, error
	Format string `yaml:"format,omitempty"` // text, json
	File   string `yaml:"file,omitempty"`
}

// State represents deployment state
type State struct {
	LastCommit     string `json:"last_commit"`
	LastDeployTime string `json:"last_deploy_time"`
	Version        string `json:"version"`
}
