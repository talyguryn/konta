// +build !windows

package lock

import (
	"fmt"
	"syscall"
)

func acquireLock(fd uintptr) error {
	if err := syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("failed to acquire lock: another instance is running")
	}
	return nil
}

func releaseLock(fd uintptr) error {
	if err := syscall.Flock(int(fd), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	return nil
}
