//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// checkPluginsDirPerms refuses any directory where someone other than the
// owner can write. The owner must be root or the running process; otherwise
// a different user could swap files in the dir between checks and exec.
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
