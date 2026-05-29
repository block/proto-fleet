//go:build windows

package main

import (
	"context"
	"log/slog"
)

// Windows go-plugin children share a job object that auto-terminates with
// the parent, so no manual reaping is needed.
func reapOrphanedPlugins(_ context.Context, _ string, _ *slog.Logger) {}
