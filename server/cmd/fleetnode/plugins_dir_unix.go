//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// checkPluginsDirPerms rejects dirs where someone other than root or the
// running uid could plant an executable between our check and exec.
func checkPluginsDirPerms(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat plugins dir %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugins dir %s is not a directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("plugins dir %s: unsupported stat type %T", path, info.Sys())
	}
	uid := uint32(os.Getuid()) //nolint:gosec // os.Getuid() is non-negative on Unix
	if stat.Uid != 0 && stat.Uid != uid {
		return fmt.Errorf("plugins dir %s: owner uid %d must be 0 (root) or %d (this process)", path, stat.Uid, uid)
	}
	if mode := info.Mode().Perm(); mode&0o022 != 0 {
		return fmt.Errorf("plugins dir %s: mode %#o must not be group- or world-writable", path, mode)
	}
	return nil
}

// Every regular file is checked, not just executables: a non-exec file
// owned by another user can be chmod +x'd between validation and load,
// becoming RCE under the agent uid.
func validatePluginFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read plugins dir %s: %w", dir, err)
	}
	uid := uint32(os.Getuid()) //nolint:gosec // os.Getuid() is non-negative on Unix
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		t := entry.Type()
		if t&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin %s is a symlink; refuse to follow", path)
		}
		if t.IsDir() {
			continue
		}
		if !t.IsRegular() {
			return fmt.Errorf("plugin %s is not a regular file (mode %s)", path, t)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		mode := info.Mode()
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("plugin %s: unsupported stat type %T", path, info.Sys())
		}
		if stat.Uid != 0 && stat.Uid != uid {
			return fmt.Errorf("plugin %s: owner uid %d must be 0 (root) or %d (this process)", path, stat.Uid, uid)
		}
		if mode.Perm()&0o022 != 0 {
			return fmt.Errorf("plugin %s: mode %#o must not be group- or world-writable", path, mode.Perm())
		}
	}
	return nil
}

// Without ancestor validation a writable parent lets another user swap the
// validated plugins dir between resolvePluginsDir and the loader's exec.
// Both the original and resolved chains are walked: the original catches
// swappable symlinks (via their containing dir's perms), the resolved
// catches a trusted symlink pointing into an attacker-writable target.
func validatePathChain(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("validatePathChain requires absolute path, got %q", path)
	}
	if err := walkAncestors(path); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	if resolved != path {
		if err := walkAncestors(resolved); err != nil {
			return err
		}
	}
	return nil
}

func walkAncestors(path string) error {
	uid := uint32(os.Getuid()) //nolint:gosec // os.Getuid() is non-negative on Unix
	current := filepath.Dir(path)
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("lstat path component %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Symlink components are guarded transitively: containing dir
			// (next iteration) blocks replacement; resolved-chain walk
			// covers the target.
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf("path component %s is not a directory", current)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("path component %s: unsupported stat type %T", current, info.Sys())
		}
		if stat.Uid != 0 && stat.Uid != uid {
			return fmt.Errorf("path component %s: owner uid %d must be 0 (root) or %d (this process)", current, stat.Uid, uid)
		}
		// /tmp-style 0o1777: sticky bit blocks non-owner delete/rename, so
		// our descendants are protected even though the dir is world-writable.
		if mode := info.Mode().Perm(); mode&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("path component %s: mode %#o must not be group- or world-writable", current, mode)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}
