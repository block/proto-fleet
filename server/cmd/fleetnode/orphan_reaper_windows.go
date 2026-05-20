//go:build windows

package main

import "log/slog"

// No-op: Windows go-plugin children share a job object that auto-terminates
// when the parent exits.
func reapOrphanedPlugins(_ string, _ *slog.Logger) {}
