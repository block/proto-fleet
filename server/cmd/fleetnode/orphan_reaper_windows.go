//go:build windows

package main

import (
	"context"
	"log/slog"
)

// No-op: Windows go-plugin children share a job object that auto-terminates
// when the parent exits. The signature matches the Unix variant so the
// shared run.go caller compiles on both platforms.
func reapOrphanedPlugins(_ context.Context, _ string, _ *slog.Logger) {}
