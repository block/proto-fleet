//go:build windows

package main

import "fmt"

// Windows plugin loading is disabled until ACL/SID validation lands.
// Refusing a present plugins dir is safer than running with no checks; an
// absent dir returns ("", nil) from resolvePluginsDir before reaching here,
// so heartbeat-only mode remains the Windows default.
func checkPluginsDirPerms(path string) error {
	return fmt.Errorf("plugin loading is not yet supported on Windows; remove %s or run fleetnode on a Unix host until Windows ACL validation is implemented", path)
}

// Unreachable while checkPluginsDirPerms refuses; no-op for cross-platform compile.
func validatePluginFiles(_ string) error { return nil }
func validatePathChain(_ string) error   { return nil }
