package service

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	defaultLinuxServiceName  = "konta"
	defaultLinuxServiceFile  = "/etc/systemd/system/konta.service"
	defaultDarwinServiceName = "com.talyguryn.konta"
	defaultDarwinServiceFile = "/Library/LaunchDaemons/com.talyguryn.konta.plist"
)

type Manager interface {
	Name() string
	FilePath() string
	Enable() error
	Disable() error
	Start() error
	Stop() error
	Restart() error
	IsRunning() bool
	StatusOutput() (string, error)
}

func NewManager(binaryPath string) Manager {
	if runtime.GOOS == "darwin" {
		return &darwinManager{
			serviceName: defaultDarwinServiceName,
			serviceFile: defaultDarwinServiceFile,
			binaryPath:  binaryPath,
		}
	}

	return &linuxManager{
		serviceName: defaultLinuxServiceName,
		serviceFile: defaultLinuxServiceFile,
		binaryPath:  binaryPath,
	}
}

func requireRoot(action string) error {
	if os.Getuid() != 0 {
		return fmt.Errorf("root privileges required to %s daemon", action)
	}
	return nil
}

func combinedOutputString(cmd *exec.Cmd) (string, error) {
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
