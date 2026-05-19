//go:build windows

package main

import (
	"fmt"
	"os"
)

// On Windows the MSI installer places the plugins directory under a path
// that requires Administrator rights to modify. A proper ACL inspection is
// out of scope here; this check only verifies the path exists and is a
// directory. Operators on Windows should rely on the installer-controlled
// default rather than passing --plugins-dir from a user shell.
func checkPluginsDirPerms(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat plugins dir %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugins dir %s is not a directory", path)
	}
	return nil
}
