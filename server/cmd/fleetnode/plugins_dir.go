package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// The plugin manager execs every file in the returned path, so non-owner
// write capability there is RCE-equivalent (checkPluginsDirPerms enforces).
func resolvePluginsDir(exeDir string) (string, error) {
	if exeDir == "" {
		return "", nil
	}
	candidate := filepath.Join(exeDir, "plugins")
	info, err := os.Lstat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("lstat plugins dir %s: %w", candidate, err)
	}
	// Symlink leaves add a mutable layer between validation and exec; refuse
	// so the reaper, validator, and loader all see the same path.
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("plugins dir %s is a symlink; refuse to follow", candidate)
	}
	if !info.IsDir() {
		return "", nil
	}
	if err := checkPluginsDirPerms(candidate); err != nil {
		return "", err
	}
	if err := validatePluginFiles(candidate); err != nil {
		return "", err
	}
	if err := validatePathChain(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return filepath.Dir(resolved)
}
