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

// validatePluginFiles enforces the per-file safety bar that the container
// check can't see: a writable plugin binary or a symlink to one elsewhere
// would still be RCE-equivalent under the agent uid.
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
		if mode.Perm()&0o111 == 0 {
			continue
		}
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
