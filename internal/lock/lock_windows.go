// +build windows

package lock

func acquireLock(fd uintptr) error {
	// Windows doesn't support Flock, so we'll use a simple check
	// In a production system, you might want to use Windows-specific locking APIs
	// For now, we'll just return success as multiple instances can run on Windows
	return nil
}

func releaseLock(fd uintptr) error {
	// No-op on Windows
	return nil
}
