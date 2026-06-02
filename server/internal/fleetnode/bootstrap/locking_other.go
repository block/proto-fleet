//go:build !linux && !darwin

package bootstrap

// No-ops: package is supported on Linux and macOS only; concurrent refresh
// can race state.yaml here and SaveState is not crash-durable.
func WithStateLock(_ string, fn func() error) error {
	return fn()
}

func syncDir(_ string) error {
	return nil
}
