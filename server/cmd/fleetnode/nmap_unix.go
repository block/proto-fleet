//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// POSIX-mode safety: uid + write bits + exec bit. Windows .exe doesn't
// model these; the windows version of this helper is a no-op.
func checkNmapBinaryOwnership(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("nmap %s: not executable", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("nmap %s: unsupported stat type %T", path, info.Sys())
	}
	uid := uint32(os.Getuid()) //nolint:gosec // os.Getuid() is non-negative on Unix
	if stat.Uid != 0 && stat.Uid != uid {
		return fmt.Errorf("nmap %s: owner uid %d must be 0 (root) or %d (this process)", path, stat.Uid, uid)
	}
	if mode := info.Mode().Perm(); mode&0o022 != 0 {
		return fmt.Errorf("nmap %s: mode %#o must not be group- or world-writable", path, mode)
	}
	return nil
}
