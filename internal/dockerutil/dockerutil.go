package dockerutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	resolvedDockerPath string
	resolveOnce        sync.Once
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

// Command creates an exec.Cmd for docker using a resolved absolute path when possible.
func Command(args ...string) *exec.Cmd {
	resolveOnce.Do(resolveDockerPath)
	return exec.Command(resolvedDockerPath, args...)
}
