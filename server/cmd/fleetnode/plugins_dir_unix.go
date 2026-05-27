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
// check can't see. Ownership and writability checks apply to every regular
// file, not just executable ones: a non-executable file owned by another
// user can be chmod +x'd by that user between validation and plugin load,
// turning it into an RCE vector under the agent uid.
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

// validatePathChain verifies that every directory above path is owned by
// root or the running uid and not group/world-writable. Without this, a
// writable ancestor lets another user swap the validated plugins dir
// between resolvePluginsDir and the loader's exec call.
//
// Both the original path and (when it differs) its symlink-resolved form
// are walked: walking the original chain catches a swappable symlink
// component (its containing dir's perms protect it from replacement);
// walking the resolved chain catches the case where a trusted symlink
// points into an attacker-writable target tree.
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
			// Symlink components reduce to two checks already in scope:
			// the symlink's containing dir (next iteration) protects it
			// from replacement, and EvalSymlinks-resolved target chain
			// validates the underlying directory.
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
		// Sticky-bit exception: dirs like /tmp are mode 0o1777 (world-
		// writable + sticky) so other users can create their own entries
		// but cannot delete or rename ours. That protects the descendants
		// we actually care about, so accept sticky+writable ancestors.
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
