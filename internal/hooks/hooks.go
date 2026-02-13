package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/talyguryn/konta/internal/logger"
)

// Runner manages hook execution
type Runner struct {
	hookPaths map[string]string
	repoDir   string
}

// New creates a new hook runner
func New(repoDir string, prePath, successPath, failurePath, postUpdatePath string) *Runner {
	return &Runner{
		hookPaths: map[string]string{
			"pre":         prePath,
			"success":     successPath,
			"failure":     failurePath,
			"post_update": postUpdatePath,
		},
		repoDir: repoDir,
	}
}

// RunPre runs the pre-deploy hook
func (r *Runner) RunPre() error {
	return r.run("pre")
}

// RunSuccess runs the success hook
func (r *Runner) RunSuccess() error {
	return r.run("success")
}

// RunFailure runs the failure hook
func (r *Runner) RunFailure() error {
	return r.run("failure")
}

// RunPostUpdate runs the post-update hook (executed after konta binary update)
func (r *Runner) RunPostUpdate() error {
	return r.run("post_update")
}

func (r *Runner) run(hookType string) error {
	hookPath := r.hookPaths[hookType]
	if hookPath == "" {
		logger.Debug("No %s hook configured", hookType)
		return nil
	}

	// Resolve hook path relative to repo directory
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(r.repoDir, hookPath)
	}

	// Check if hook file exists
	if _, err := os.Stat(hookPath); err != nil {
		logger.Warn("Hook file not found: %s", hookPath)
		return nil
	}

	logger.Debug("Running %s hook: %s", hookType, hookPath)

	cmd := exec.Command("bash", hookPath)
	cmd.Dir = r.repoDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s hook failed: %w", hookType, err)
	}

	logger.Debug("%s hook executed successfully", hookType)
	return nil
}
