package dockerutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type Client interface {
	Command(args ...string) *exec.Cmd
	ComposeCommand(args ...string) *exec.Cmd
}

type client struct{}

type composeMode int

const (
	composeViaDocker composeMode = iota
	composeViaStandalone
)

var (
	resolvedDockerPath string
	resolveOnce        sync.Once
	resolvedComposeBin string
	resolveComposeOnce sync.Once
	resolvedComposeVia composeMode
	defaultClient      Client = client{}
)

func resolveDockerPath() {
	if path, err := exec.LookPath("docker"); err == nil {
		resolvedDockerPath = path
		return
	}

	candidates := []string{
		"/usr/local/bin/docker",
		"/opt/homebrew/bin/docker",
		"/Applications/Docker.app/Contents/Resources/bin/docker",
		"/usr/bin/docker",
		"/bin/docker",
	}

	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		candidates = append(candidates, filepath.Join("/Users", sudoUser, ".docker", "bin", "docker"))
	}

	if user := os.Getenv("USER"); user != "" {
		candidates = append(candidates, filepath.Join("/Users", user, ".docker", "bin", "docker"))
	}

	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, ".docker", "bin", "docker"))
	}

	if matches, err := filepath.Glob("/Users/*/.docker/bin/docker"); err == nil {
		candidates = append(candidates, matches...)
	}

	for _, candidate := range candidates {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			resolvedDockerPath = candidate
			return
		}
	}

	// Keep default command name as last resort.
	resolvedDockerPath = "docker"
}

func resolveComposePath() {
	resolveOnce.Do(resolveDockerPath)

	// Prefer native `docker compose` when supported by the resolved docker CLI.
	if err := exec.Command(resolvedDockerPath, "compose", "version").Run(); err == nil {
		resolvedComposeVia = composeViaDocker
		resolvedComposeBin = resolvedDockerPath
		return
	}

	if path, err := exec.LookPath("docker-compose"); err == nil {
		resolvedComposeVia = composeViaStandalone
		resolvedComposeBin = path
		return
	}

	candidates := []string{
		"/usr/local/bin/docker-compose",
		"/opt/homebrew/bin/docker-compose",
		"/usr/bin/docker-compose",
		"/bin/docker-compose",
	}

	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		candidates = append(candidates, filepath.Join("/Users", sudoUser, ".docker", "cli-plugins", "docker-compose"))
	}

	if user := os.Getenv("USER"); user != "" {
		candidates = append(candidates, filepath.Join("/Users", user, ".docker", "cli-plugins", "docker-compose"))
	}

	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, ".docker", "cli-plugins", "docker-compose"))
	}

	for _, candidate := range candidates {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			resolvedComposeVia = composeViaStandalone
			resolvedComposeBin = candidate
			return
		}
	}

	// Last resort: keep using `docker compose` for backward compatibility.
	resolvedComposeVia = composeViaDocker
	resolvedComposeBin = resolvedDockerPath
}

// Command creates an exec.Cmd for docker using a resolved absolute path when possible.
func Command(args ...string) *exec.Cmd {
	return defaultClient.Command(args...)
}

// NewClient returns a docker client implementation that resolves docker binary path once.
func NewClient() Client {
	return client{}
}

// ComposeCommand creates an exec.Cmd for `docker compose` using a resolved absolute docker path.
func ComposeCommand(args ...string) *exec.Cmd {
	return defaultClient.ComposeCommand(args...)
}

func (client) Command(args ...string) *exec.Cmd {
	resolveOnce.Do(resolveDockerPath)
	return exec.Command(resolvedDockerPath, args...)
}

func (runner client) ComposeCommand(args ...string) *exec.Cmd {
	resolveComposeOnce.Do(resolveComposePath)

	if resolvedComposeVia == composeViaStandalone {
		if len(args) > 0 && args[0] == "compose" {
			args = args[1:]
		}
		return exec.Command(resolvedComposeBin, args...)
	}

	if len(args) > 0 && args[0] == "compose" {
		return runner.Command(args...)
	}

	composeArgs := append([]string{"compose"}, args...)
	return runner.Command(composeArgs...)
}
