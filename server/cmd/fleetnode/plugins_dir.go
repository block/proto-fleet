package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolvePluginsDir returns the plugins directory or "" when no usable
// directory exists. A missing binary-adjacent default is silent (heartbeat
// only); a present but unsafe default is a hard error so the operator who
// shipped plugins learns about it. Any returned non-empty path has passed
// checkPluginsDirPerms — the plugin manager execs everything in this
// directory, so non-owner write capability there is RCE-equivalent.
func resolvePluginsDir(flag, exeDir string) (string, error) {
	if flag != "" {
		if !filepath.IsAbs(flag) {
			return "", fmt.Errorf("--plugins-dir must be an absolute path, got %q", flag)
		}
		if err := checkPluginsDirPerms(flag); err != nil {
			return "", err
		}
		return flag, nil
	}
	if exeDir == "" {
		return "", nil
	}
	candidate := filepath.Join(exeDir, "plugins")
	info, err := os.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat plugins dir %s: %w", candidate, err)
	}
	if !info.IsDir() {
		return "", nil
	}
	if err := checkPluginsDirPerms(candidate); err != nil {
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
