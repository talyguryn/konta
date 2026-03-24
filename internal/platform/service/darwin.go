package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type darwinManager struct {
	serviceName string
	serviceFile string
	binaryPath  string
}

func (m *darwinManager) Name() string {
	return m.serviceName
}

func (m *darwinManager) FilePath() string {
	return m.serviceFile
}

func (m *darwinManager) Enable() error {
	if err := requireRoot("enable"); err != nil {
		return err
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>run</string>
		<string>--watch</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Applications/Docker.app/Contents/Resources/bin</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>/var/log/konta/konta.log</string>
	<key>StandardErrorPath</key>
	<string>/var/log/konta/konta.error.log</string>
</dict>
</plist>
`, m.serviceName, m.binaryPath)

	if err := os.MkdirAll("/var/log/konta", 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	if err := os.MkdirAll("/Library/LaunchDaemons", 0755); err != nil {
		return fmt.Errorf("failed to create LaunchDaemons directory: %w", err)
	}
	if err := os.WriteFile(m.serviceFile, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	_ = exec.Command("launchctl", "bootout", "system/"+m.serviceName).Run()
	if output, err := combinedOutputString(exec.Command("launchctl", "bootstrap", "system", m.serviceFile)); err != nil {
		return fmt.Errorf("failed to bootstrap launchd service: %w (output: %s)", err, output)
	}
	_ = exec.Command("launchctl", "enable", "system/"+m.serviceName).Run()
	if output, err := combinedOutputString(exec.Command("launchctl", "kickstart", "-k", "system/"+m.serviceName)); err != nil {
		return fmt.Errorf("failed to start launchd service: %w (output: %s)", err, output)
	}

	return nil
}

func (m *darwinManager) Disable() error {
	if err := requireRoot("disable"); err != nil {
		return err
	}

	_ = exec.Command("launchctl", "bootout", "system/"+m.serviceName).Run()
	if err := os.Remove(m.serviceFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	return nil
}

func (m *darwinManager) Start() error {
	if err := requireRoot("start"); err != nil {
		return err
	}
	if output, err := combinedOutputString(exec.Command("launchctl", "kickstart", "-k", "system/"+m.serviceName)); err != nil {
		return fmt.Errorf("failed to start service: %w (output: %s)", err, output)
	}
	return nil
}

func (m *darwinManager) Stop() error {
	if err := requireRoot("stop"); err != nil {
		return err
	}
	if output, err := combinedOutputString(exec.Command("launchctl", "kill", "SIGTERM", "system/"+m.serviceName)); err != nil {
		return fmt.Errorf("failed to stop service: %w (output: %s)", err, output)
	}
	return nil
}

func (m *darwinManager) Restart() error {
	if err := requireRoot("restart"); err != nil {
		return err
	}
	if output, err := combinedOutputString(exec.Command("launchctl", "kickstart", "-k", "system/"+m.serviceName)); err != nil {
		return fmt.Errorf("failed to restart service: %w (output: %s)", err, output)
	}
	return nil
}

func (m *darwinManager) IsRunning() bool {
	return exec.Command("launchctl", "print", "system/"+m.serviceName).Run() == nil
}

func (m *darwinManager) StatusOutput() (string, error) {
	output, err := exec.Command("launchctl", "print", "system/"+m.serviceName).CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
