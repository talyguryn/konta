package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/talyguryn/konta/internal/logger"
	platformservice "github.com/talyguryn/konta/internal/platform/service"
)

func isDaemonCurrentlyRunning() bool {
	return daemonManager().IsRunning()
}

func restartDaemonForCurrentOS() error {
	return daemonManager().Restart()
}

// daemonManager returns platform-specific daemon manager implementation.
func daemonManager() platformservice.Manager {
	binaryPath, err := os.Executable()
	if err != nil || strings.TrimSpace(binaryPath) == "" {
		binaryPath = "/usr/local/bin/konta"
	}
	return platformservice.NewManager(binaryPath)
}

// ManageDaemon manages the system daemon (systemd on Linux, launchd on macOS)
func ManageDaemon(action string) error {
	manager := daemonManager()

	switch strings.ToLower(action) {
	case "enable":
		return daemonEnable(manager)

	case "disable":
		return daemonDisable(manager)

	case "status":
		return daemonStatus(manager)

	case "start":
		logger.Warn("Daemon action 'start' is deprecated. Use 'enable' instead.")
		return daemonEnable(manager)

	case "stop":
		logger.Warn("Daemon action 'stop' is deprecated. Use 'disable' instead.")
		return daemonDisable(manager)

	case "restart":
		return daemonRestart(manager)

	default:
		return fmt.Errorf("unknown daemon action: %s (use: enable, disable, restart, status)", action)
	}
}

func daemonEnable(manager platformservice.Manager) error {
	if err := manager.Enable(); err != nil {
		return err
	}
	fmt.Printf("✓ Konta daemon enabled and started\n")
	return nil
}

func daemonDisable(manager platformservice.Manager) error {
	if err := manager.Disable(); err != nil {
		return err
	}
	fmt.Printf("✓ Konta daemon disabled\n")
	return nil
}

func daemonStatus(manager platformservice.Manager) error {
	if !manager.IsRunning() {
		fmt.Printf("✗ Konta daemon is not running\n")
		return nil
	}

	fmt.Printf("✓ Konta daemon is running\n")
	if output, err := manager.StatusOutput(); err == nil && strings.TrimSpace(output) != "" {
		fmt.Printf("%s\n", strings.TrimSpace(output))
	}
	return nil
}

func daemonStart(manager platformservice.Manager) error {
	if err := manager.Start(); err != nil {
		return err
	}
	fmt.Printf("✓ Konta daemon started\n")
	return nil
}

func daemonStop(manager platformservice.Manager) error {
	if err := manager.Stop(); err != nil {
		return err
	}
	fmt.Printf("✓ Konta daemon stopped\n")
	return nil
}

func daemonRestart(manager platformservice.Manager) error {
	if err := manager.Restart(); err != nil {
		return err
	}
	fmt.Printf("✓ Konta daemon restarted\n")
	return nil
}
