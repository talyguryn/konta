package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

func listDesiredProjectsForStatePrune(appsDir string) ([]string, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, err
	}

	projects := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		composePath := filepath.Join(appsDir, entry.Name(), "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			projects = append(projects, entry.Name())
		}
	}

	return projects, nil
}

// findProjectsMarkedForRecreate scans apps directory and returns projects that have konta.recreate=true label.
// This is an optional feature for projects that require forced recreation on every deployment cycle.
func findProjectsMarkedForRecreate(appsDir string) ([]string, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var recreateProjects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectName := entry.Name()
		composePath := filepath.Join(appsDir, projectName, "docker-compose.yml")

		data, err := os.ReadFile(composePath)
		if err != nil {
			continue // No compose file, skip
		}

		if composeHasRecreateLabel(data) {
			recreateProjects = append(recreateProjects, projectName)
		}
	}

	sort.Strings(recreateProjects)
	return recreateProjects, nil
}

func composeHasRecreateLabel(data []byte) bool {
	// Match only explicit YAML label entries, not comments or arbitrary text.
	// Examples matched:
	//   - konta.recreate=true
	//   - "konta.recreate=true"
	//   - 'konta.recreate=true'
	pattern := regexp.MustCompile(`(?im)^\s*-\s*["']?konta\.recreate\s*=\s*true["']?\s*$`)
	return pattern.Match(data)
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
