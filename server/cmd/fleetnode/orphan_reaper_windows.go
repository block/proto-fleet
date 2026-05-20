//go:build windows

package main

import "log/slog"

// On Windows, go-plugin children inherit a job object that terminates them
// when the parent exits, so we don't need a manual reaper. A proper ACL-
// aware enumeration is out of scope here; this is a documented no-op.
func reapOrphanedPlugins(_ string, _ *slog.Logger) {}
