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
func New(repoDir string, startedPath, prePath, successPath, failurePath, postUpdatePath string) *Runner {
	return &Runner{
		hookPaths: map[string]string{
			"started":     startedPath,
			"pre":         prePath,
			"success":     successPath,
			"failure":     failurePath,
			"post_update": postUpdatePath,
		},
		repoDir: repoDir,
	}
}

// RunStarted runs the started hook (when konta daemon starts)
func (r *Runner) RunStarted() error {
	return r.run("started")
}

// RunPre runs the pre-deploy hook
func (r *Runner) RunPre() error {
	return r.run("pre")
}

// RunSuccess runs the success hook
// apps: list of applications that were successfully updated
func (r *Runner) RunSuccess(apps []string) error {
	return r.run("success", apps...)
}

// RunFailure runs the failure hook
// errorMessage: the error message that caused the failure
func (r *Runner) RunFailure(errorMessage string) error {
	return r.run("failure", errorMessage)
}

// RunPostUpdate runs the post-update hook (executed after konta binary update)
func (r *Runner) RunPostUpdate() error {
	return r.run("post_update")
}

func (r *Runner) run(hookType string, args ...string) error {
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

	// Prepare command arguments: bash hook_script.sh [arg1] [arg2] ...
	cmdArgs := append([]string{hookPath}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = r.repoDir

	// Suppress output for post_update hook, show output for other hooks
	if hookType == "post_update" {
		cmd.Stdout = nil
		cmd.Stderr = nil
	} else {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s hook failed: %w", hookType, err)
	}

	logger.Debug("%s hook executed successfully", hookType)
	return nil
}
