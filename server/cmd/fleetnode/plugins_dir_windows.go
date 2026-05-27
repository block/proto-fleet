//go:build windows

package main

import "fmt"

// Windows plugin loading is disabled. The Unix path uses uid + permission-
// mask checks (plus symlink rejection) to refuse RCE-equivalent
// configurations before exec'ing adjacent plugin binaries. Equivalent
// Windows ACL / SID / reparse-point validation needs golang.org/x/sys/windows
// work that hasn't landed yet; refusing a present plugins dir is safer than
// running with no validation. When the dir does not exist, resolvePluginsDir
// returns ("", nil) without reaching here, so Windows fleetnode runs in
// heartbeat-only mode by default.
func checkPluginsDirPerms(path string) error {
	return fmt.Errorf("plugin loading is not yet supported on Windows; remove %s or run fleetnode on a Unix host until Windows ACL validation is implemented", path)
}

// validatePluginFiles is unreachable while checkPluginsDirPerms refuses, but
// kept as a no-op so the shared resolvePluginsDir call sequence type-checks
// on both platforms.
func validatePluginFiles(_ string) error { return nil }
