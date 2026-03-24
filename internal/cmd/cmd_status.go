package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/talyguryn/konta/internal/logger"
	"github.com/talyguryn/konta/internal/state"
	"github.com/talyguryn/konta/internal/types"
)

// Status shows the last deployment status.
func Status(version string) error {
	fmt.Printf("Konta version: %s\n\n", version)

	manager := daemonManager()
	if !manager.IsRunning() {
		fmt.Printf("✗ Konta daemon is not running\n")
	} else {
		fmt.Printf("✓ Konta daemon is running\n")

		if runtime.GOOS == "linux" {
			uptimeCmd := exec.Command("systemctl", "show", manager.Name(), "--property=ActiveEnterTimestamp")
			if uptimeOutput, err := uptimeCmd.Output(); err == nil {
				uptimeStr := strings.TrimSpace(string(uptimeOutput))
				if strings.HasPrefix(uptimeStr, "ActiveEnterTimestamp=") {
					timestampStr := strings.TrimPrefix(uptimeStr, "ActiveEnterTimestamp=")
					if startTime, err := time.Parse("Mon 2006-01-02 15:04:05 MST", timestampStr); err == nil {
						uptime := time.Since(startTime)
						fmt.Printf("  Uptime: %s\n", formatUptime(uptime))
					}
				}
			}
		}
	}

	fmt.Println()

	currentState, err := state.Load()
	if err != nil {
		logger.Debug("Failed to load state: %v", err)
	}

	if currentState.LastCommit == "" {
		fmt.Println("Last deployment: (none yet)")
	} else {
		fmt.Println("Last deployment:")
		fmt.Printf("  Commit:    %s\n", currentState.LastCommit[:8])
		fmt.Printf("  Timestamp: %s\n", currentState.LastDeployTime)
		if strings.TrimSpace(currentState.LastAttemptedCommit) != "" {
			fmt.Printf("  Attempt:   %s (%s", shortCommitHash(currentState.LastAttemptedCommit), strings.TrimSpace(currentState.LastAttemptStatus))
			if strings.TrimSpace(currentState.LastAttemptTime) != "" {
				fmt.Printf(" at %s", currentState.LastAttemptTime)
			}
			fmt.Printf(")\n")
		}
	}

	printApplicationsByCommit(currentState)

	return nil
}

type commitDeploymentGroup struct {
	Commit     string
	DeployTime string
	Projects   []string
}

func printApplicationsByCommit(currentState *types.State) {
	if currentState == nil || len(currentState.Projects) == 0 {
		return
	}

	groups := make(map[string]*commitDeploymentGroup)
	for projectName, projectState := range currentState.Projects {
		commit := strings.TrimSpace(projectState.ActiveCommit)
		if commit == "" {
			commit = strings.TrimSpace(projectState.LastCommit)
		}
		if commit == "" {
			continue
		}

		deployTime := strings.TrimSpace(projectState.LastDeployTime)
		group, exists := groups[commit]
		if !exists {
			group = &commitDeploymentGroup{Commit: commit, DeployTime: deployTime}
			groups[commit] = group
		} else if isLaterDeployTime(deployTime, group.DeployTime) {
			group.DeployTime = deployTime
		}

		group.Projects = append(group.Projects, projectName)
	}

	if len(groups) == 0 {
		return
	}

	orderedGroups := make([]*commitDeploymentGroup, 0, len(groups))
	for _, group := range groups {
		sort.Strings(group.Projects)
		orderedGroups = append(orderedGroups, group)
	}

	sort.SliceStable(orderedGroups, func(i, j int) bool {
		leftTime, leftOk := parseDeployTimestamp(orderedGroups[i].DeployTime)
		rightTime, rightOk := parseDeployTimestamp(orderedGroups[j].DeployTime)

		if leftOk && rightOk && !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		if leftOk != rightOk {
			return rightOk
		}
		if orderedGroups[i].DeployTime != orderedGroups[j].DeployTime {
			return orderedGroups[i].DeployTime < orderedGroups[j].DeployTime
		}
		return orderedGroups[i].Commit < orderedGroups[j].Commit
	})

	fmt.Println()
	fmt.Println("Applications deployed by commits:")
	for _, group := range orderedGroups {
		deployTime := strings.TrimSpace(group.DeployTime)
		if deployTime == "" {
			deployTime = "unknown"
		}
		fmt.Printf("  %s — %s\n", shortCommitHash(group.Commit), deployTime)

		for _, projectName := range group.Projects {
			fmt.Printf("  - %s\n", projectName)
		}
		fmt.Println()
	}
}

func parseDeployTimestamp(timestamp string) (time.Time, bool) {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp == "" {
		return time.Time{}, false
	}

	parsed, err := time.Parse("2006-01-02 15:04:05", timestamp)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func isLaterDeployTime(candidate string, baseline string) bool {
	candidateTime, candidateOk := parseDeployTimestamp(candidate)
	baselineTime, baselineOk := parseDeployTimestamp(baseline)

	if candidateOk && baselineOk {
		return candidateTime.After(baselineTime)
	}
	if candidateOk != baselineOk {
		return candidateOk
	}

	return strings.TrimSpace(candidate) > strings.TrimSpace(baseline)
}

// formatUptime formats a duration into a human-readable uptime string.
func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}