//go:build windows

package main

import (
	"fmt"
	"os"
)

// Windows relies on the MSI installer placing plugins under an
// Administrator-only path; an ACL check is out of scope here.
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
