package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/talyguryn/konta/internal/cmd"
	"github.com/talyguryn/konta/internal/logger"
)

const Version = "0.1.45"

func main() {
	if len(os.Args) < 2 {
		cmd.PrintUsage(Version)
		os.Exit(0)
	}

	command := os.Args[1]

	// Initialize logger
	if err := logger.Init(""); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(command) {
	case "help", "-h", "--help":
		cmd.PrintUsage(Version)
		os.Exit(0)

	case "version", "-v", "--version":
		fmt.Printf("Konta v%s\n", Version)
		os.Exit(0)

	case "bootstrap":
		bootstrapArgs := os.Args[2:]
		if err := cmd.Bootstrap(bootstrapArgs); err != nil {
			logger.Fatal("Bootstrap failed: %v", err)
		}

	case "uninstall":
		if err := cmd.Uninstall(); err != nil {
			logger.Fatal("Uninstall failed: %v", err)
		}

	case "update":
		args := os.Args[2:]
		forceYes := false
		for _, arg := range args {
			if arg == "-y" || arg == "--yes" {
				forceYes = true
				break
			}
		}
		if err := cmd.Update(Version, forceYes); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

	case "run", "-r":
		args := os.Args[2:]
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
		if err := cmd.Run(dryRun, watch, Version); err != nil {
			logger.Fatal("Run failed: %v", err)
		}

	case "-d":
		// Short flag for 'daemon enable'
		if err := cmd.ManageDaemon("enable"); err != nil {
			logger.Fatal("Daemon management failed: %v", err)
		}

	case "daemon":
		if len(os.Args) < 3 {
			fmt.Println("Usage: konta daemon [enable|disable|restart|status]")
			os.Exit(1)
		}
		subcmd := os.Args[2]
		if err := cmd.ManageDaemon(subcmd); err != nil {
			logger.Fatal("Daemon management failed: %v", err)
		}

	case "start":
		logger.Warn("Command 'start' is deprecated. Use 'enable' instead.")
		if err := cmd.ManageDaemon("enable"); err != nil {
			logger.Fatal("Enable failed: %v", err)
		}

	case "stop":
		logger.Warn("Command 'stop' is deprecated. Use 'disable' instead.")
		if err := cmd.ManageDaemon("disable"); err != nil {
			logger.Fatal("Disable failed: %v", err)
		}

	case "enable":
		if err := cmd.ManageDaemon("enable"); err != nil {
			logger.Fatal("Enable failed: %v", err)
		}

	case "disable":
		if err := cmd.ManageDaemon("disable"); err != nil {
			logger.Fatal("Disable failed: %v", err)
		}

	case "restart":
		if err := cmd.ManageDaemon("restart"); err != nil {
			logger.Fatal("Restart failed: %v", err)
		}

	case "status", "-s":
		if err := cmd.Status(); err != nil {
			logger.Fatal("Status failed: %v", err)
		}

	case "journal":
		if err := cmd.Journal(); err != nil {
			logger.Fatal("Journal failed: %v", err)
		}

	case "config":
		edit := false
		for _, arg := range os.Args[2:] {
			if arg == "-e" || arg == "--edit" {
				edit = true
				break
			}
		}
		if err := cmd.Config(edit); err != nil {
			logger.Fatal("Config failed: %v", err)
		}

	case "-j", "-J":
		if err := cmd.Journal(); err != nil {
			logger.Fatal("Journal failed: %v", err)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		cmd.PrintUsage(Version)
		os.Exit(1)
	}
}
