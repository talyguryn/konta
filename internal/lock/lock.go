package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/talyguryn/konta/internal/logger"
)

var lockPath string

func init() {
	lockPath = "/var/run/konta.lock"
	// Fallback to temp directory if /var/run is not writable
	if f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644); err != nil {
		homeDir, _ := os.UserHomeDir()
		if homeDir == "" {
			homeDir = "/tmp"
		}
		lockPath = filepath.Join(homeDir, ".konta", "konta.lock")
	} else {
		f.Close()
		os.Remove(lockPath)
	}
}

type FileLock struct {
	file *os.File
}

// Acquire acquires the file lock
func Acquire() (*FileLock, error) {
	// Make sure directory exists
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire the lock
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		logger.Warn("Another Konta instance is running")
		return nil, fmt.Errorf("failed to acquire lock: another instance is running")
	}

	logger.Debug("Lock acquired")
	return &FileLock{file: file}, nil
}

// Release releases the file lock
func (fl *FileLock) Release() error {
	if fl.file == nil {
		return nil
	}

	if err := syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	if err := fl.file.Close(); err != nil {
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	logger.Debug("Lock released")
	return nil
}
