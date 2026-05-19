package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolvePluginsDir picks the plugins directory the control loop should use.
//
// Explicit flag: must be an absolute path; perms are enforced.
// No flag:       look for <exeDir>/plugins. Missing dir means "no control
//
//	loop, heartbeat only" so operators get a heartbeat-only
//	daemon out of the box. A present but unsafe dir is a hard
//	error — silently downgrading would hide misconfiguration
//	that an operator who shipped plugins probably wants to know
//	about.
//
// Any returned non-empty path has passed checkPluginsDirPerms: the plugin
// manager execs everything in this directory as the agent's user, so any
// non-owner write capability is RCE-equivalent.
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

// executableDir returns the directory containing the running binary, or ""
// if os.Executable fails (e.g. on platforms where it isn't supported).
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
