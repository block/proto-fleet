//go:build !linux && !darwin

package main

// withStateLock and syncDir are no-ops on platforms without flock /
// directory-handle Sync; the agent CLI is officially supported on Linux
// and macOS only. Concurrent refresh on these platforms can race
// state.yaml writes, and saveState is not crash-durable.
func withStateLock(_ string, fn func() error) error {
	return fn()
}

func syncDir(_ string) error {
	return nil
}
