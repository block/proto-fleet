package command

import "context"

type commandActivitySuppressedKey struct{}

// WithCommandActivitySuppressed marks an internal command dispatch whose
// command-batch state should still be tracked, but whose device-command
// activity rows should not be visible in the activity feed.
func WithCommandActivitySuppressed(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandActivitySuppressedKey{}, true)
}

// CommandActivitySuppressed reports whether command activity logging is
// disabled for this context.
func CommandActivitySuppressed(ctx context.Context) bool {
	suppressed, _ := ctx.Value(commandActivitySuppressedKey{}).(bool)
	return suppressed
}
