package types

// Config represents the konta configuration
type Config struct {
	Version       string         `yaml:"version"`
	Repository    RepositoryConf `yaml:"repository"`
	Deploy        DeployConf     `yaml:"deploy,omitempty"`
	Hooks         HooksConf      `yaml:"hooks,omitempty"`
	Logging       LoggingConf    `yaml:"logging,omitempty"`
	KontaUpdates  string         `yaml:"konta_updates,omitempty"` // auto, notify (default), false
}

// RepositoryConf represents git repository configuration
type RepositoryConf struct {
	URL      string `yaml:"url"`
	Branch   string `yaml:"branch"`
	Token    string `yaml:"token"`
	Path     string `yaml:"path"` // Path to base directory containing 'apps' folder (or just empty/. for repo root)
	Interval int    `yaml:"interval"` // seconds
}

// DeployConf represents deployment configuration
type DeployConf struct {
	Atomic bool `yaml:"atomic,omitempty"`
	Parallel bool `yaml:"parallel,omitempty"`
	DryRun bool `yaml:"dry_run,omitempty"`
	// RemoveOrphans is always enabled by default to keep disk space clean
}

// HooksConf represents hooks configuration
type HooksConf struct {
	Started    string `yaml:"started,omitempty"`     // Just filename: started.sh (found in hooks dir)
	Pre        string `yaml:"pre,omitempty"`        // Just filename: pre.sh (found in hooks dir)
	Success    string `yaml:"success,omitempty"`    // Just filename: success.sh (found in hooks dir)
	Failure    string `yaml:"failure,omitempty"`    // Just filename: failure.sh (found in hooks dir)
	PostUpdate string `yaml:"post_update,omitempty"` // Just filename: post_update.sh (found in hooks dir)
	StartedAbs string `yaml:"-"` // Absolute path to started hook (set by config loader)
	PreAbs     string `yaml:"-"` // Absolute path to pre hook (set by config loader)
	SuccessAbs string `yaml:"-"` // Absolute path to success hook
	FailureAbs string `yaml:"-"` // Absolute path to failure hook
	PostUpdateAbs string `yaml:"-"` // Absolute path to post_update hook
}

// LoggingConf represents logging configuration
type LoggingConf struct {
	Level  string `yaml:"level,omitempty"` // debug, info, warn, error
	Format string `yaml:"format,omitempty"` // text, json
	File   string `yaml:"file,omitempty"`
}

// State represents deployment state
type State struct {
	LastCommit     string                 `json:"last_commit"`
	LastDeployTime string                 `json:"last_deploy_time"`
	Version        string                 `json:"version"`
	Projects       map[string]ProjectState `json:"projects,omitempty"` // Per-project state for change detection
}

// ProjectState represents the state of an individual project
type ProjectState struct {
	LastCommit     string `json:"last_commit"`      // Last commit that affected this project
	LastDeployTime string `json:"last_deploy_time"` // When this project was last deployed
}
