package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/talyguryn/konta/internal/cmd"
	"github.com/talyguryn/konta/internal/logger"
)

type App struct {
	version string
}

func New(version string) *App {
	return &App{version: version}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		cmd.PrintUsage(a.version)
		return 0
	}

	if err := logger.Init(""); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return 1
	}

	command := strings.ToLower(args[0])

	switch command {
	case "help", "-h", "--help":
		cmd.PrintUsage(a.version)
		return 0

	case "version", "-v", "--version":
		fmt.Printf("Konta v%s\n", a.version)
		return 0

	case "bootstrap":
		if err := cmd.Bootstrap(args[1:]); err != nil {
			logger.Fatal("Bootstrap failed: %v", err)
		}
		return 0

	case "uninstall":
		if err := cmd.Uninstall(); err != nil {
			logger.Fatal("Uninstall failed: %v", err)
		}
		return 0

	case "update":
		forceYes, releaseChannel := parseUpdateArgs(args[1:])
		if err := cmd.Update(a.version, forceYes, releaseChannel); err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		return 0

	case "run", "-r":
		dryRun, watch := parseRunArgs(args[1:])
		if err := cmd.Run(dryRun, watch, a.version); err != nil {
			logger.Fatal("Run failed: %v", err)
		}
		return 0

	case "deploy":
		dryRun := parseDeployArgs(args[1:])
		if err := cmd.Deploy(dryRun, a.version); err != nil {
			logger.Fatal("Deploy failed: %v", err)
		}
		return 0

	case "-d":
		if err := cmd.ManageDaemon("enable"); err != nil {
			logger.Fatal("Daemon management failed: %v", err)
		}
		return 0

	case "daemon":
		if len(args) < 2 {
			fmt.Println("Usage: konta daemon [enable|disable|restart|status]")
			return 1
		}
		if err := cmd.ManageDaemon(args[1]); err != nil {
			logger.Fatal("Daemon management failed: %v", err)
		}
		return 0

	case "start":
		logger.Warn("Command 'start' is deprecated. Use 'enable' instead.")
		if err := cmd.ManageDaemon("enable"); err != nil {
			logger.Fatal("Enable failed: %v", err)
		}
		return 0

	case "stop":
		logger.Warn("Command 'stop' is deprecated. Use 'disable' instead.")
		if err := cmd.ManageDaemon("disable"); err != nil {
			logger.Fatal("Disable failed: %v", err)
		}
		return 0

	case "enable":
		if err := cmd.ManageDaemon("enable"); err != nil {
			logger.Fatal("Enable failed: %v", err)
		}
		return 0

	case "disable":
		if err := cmd.ManageDaemon("disable"); err != nil {
			logger.Fatal("Disable failed: %v", err)
		}
		return 0

	case "restart":
		if err := cmd.ManageDaemon("restart"); err != nil {
			logger.Fatal("Restart failed: %v", err)
		}
		return 0

	case "status", "-s":
		if err := cmd.Status(a.version); err != nil {
			logger.Fatal("Status failed: %v", err)
		}
		return 0

	case "journal", "-j", "-J":
		if err := cmd.Journal(); err != nil {
			logger.Fatal("Journal failed: %v", err)
		}
		return 0

	case "config":
		if err := cmd.Config(parseConfigArgs(args[1:])); err != nil {
			logger.Fatal("Config failed: %v", err)
		}
		return 0

	default:
		fmt.Printf("Unknown command: %s\n", args[0])
		cmd.PrintUsage(a.version)
		return 1
	}
}

func parseUpdateArgs(args []string) (bool, string) {
	forceYes := false
	releaseChannel := ""
	for _, arg := range args {
		if arg == "-y" || arg == "--yes" {
			forceYes = true
		}
	}
	for index := 0; index < len(args); index++ {
		if args[index] == "--channel" && index+1 < len(args) {
			releaseChannel = args[index+1]
			index++
		}
	}
	return forceYes, releaseChannel
}

func parseRunArgs(args []string) (bool, bool) {
	dryRun := false
	watch := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--watch":
			watch = true
		}
	}
	return dryRun, watch
}

func parseDeployArgs(args []string) bool {
	for _, arg := range args {
		if arg == "--dry-run" {
			return true
		}
	}
	return false
}

func parseConfigArgs(args []string) bool {
	for _, arg := range args {
		if arg == "-e" || arg == "--edit" {
			return true
		}
	}
	return false
}
