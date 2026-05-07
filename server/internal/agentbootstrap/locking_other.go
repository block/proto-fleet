//go:build !linux && !darwin

package agentbootstrap

// WithStateLock and syncDir are no-ops on platforms without flock /
// directory-handle Sync; the package is officially supported on Linux and
// macOS only. Concurrent refresh on these platforms can race state.yaml
// writes, and SaveState is not crash-durable.
func WithStateLock(_ string, fn func() error) error {
	return fn()
}

func syncDir(_ string) error {
	return nil
}
