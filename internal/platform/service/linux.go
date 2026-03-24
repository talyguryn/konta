package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type linuxManager struct {
	serviceName string
	serviceFile string
	binaryPath  string
}

func (m *linuxManager) Name() string {
	return m.serviceName
}

func (m *linuxManager) FilePath() string {
	return m.serviceFile
}

func (m *linuxManager) Enable() error {
	if err := requireRoot("enable"); err != nil {
		return err
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Konta GitOps for Docker Compose
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
ExecStart=%s run --watch
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
`, m.binaryPath)

	if err := os.WriteFile(m.serviceFile, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}
	if err := exec.Command("systemctl", "enable", m.serviceName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	if err := exec.Command("systemctl", "start", m.serviceName).Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (m *linuxManager) Disable() error {
	if err := requireRoot("disable"); err != nil {
		return err
	}

	_ = exec.Command("systemctl", "stop", m.serviceName).Run()
	if err := exec.Command("systemctl", "disable", m.serviceName).Run(); err != nil {
		return fmt.Errorf("failed to disable service: %w", err)
	}
	if err := os.Remove(m.serviceFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove service file: %w", err)
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	return nil
}

func (m *linuxManager) Start() error {
	if err := requireRoot("start"); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "start", m.serviceName).Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	return nil
}

func (m *linuxManager) Stop() error {
	if err := requireRoot("stop"); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "stop", m.serviceName).Run(); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	return nil
}

func (m *linuxManager) Restart() error {
	if err := requireRoot("restart"); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "restart", m.serviceName).Run(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}
	return nil
}

func (m *linuxManager) IsRunning() bool {
	output, err := exec.Command("systemctl", "is-active", m.serviceName).Output()
	return err == nil && strings.TrimSpace(string(output)) == "active"
}

func (m *linuxManager) StatusOutput() (string, error) {
	output, err := exec.Command("systemctl", "status", m.serviceName, "--no-pager").CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
