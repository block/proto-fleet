//go:build linux || darwin

package agentbootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Serializes concurrent refreshes via flock on <dir>/state.lock so a slower
// writer can't clobber a newer state.yaml. Refuses to follow a symlink at
// the dir leaf so the lock can't land in an attacker-chosen location and
// silently break serialization for the SaveState that follows.
func WithStateLock(dir string, fn func() error) error {
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state dir %s is a symlink; refusing to take a lock through it", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := tightenStateDirPerms(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open state lock: %w", err)
	}
	// Kernel releases flock on close; explicit LOCK_UN would race with the
	// deferred Close.
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	return fn()
}

// fsyncs a directory so a preceding os.Rename is durable across a power
// loss. POSIX only: Windows directory handles do not support
// FlushFileBuffers.
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open dir for sync: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync dir: %w", err)
	}
	return nil
}
