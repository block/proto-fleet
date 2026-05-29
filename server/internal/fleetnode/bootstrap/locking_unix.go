//go:build linux || darwin

package bootstrap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// WithStateLock serializes refreshes via flock on <dir>/state.lock. Lstat
// rejects symlink leaves so an attacker can't redirect the lock to a path
// they control and break serialization of the following SaveState. LOCK_NB
// fails fast with a useful error (naming the recorded holder pid) instead
// of blocking forever.
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
	lockPath := filepath.Join(dir, "state.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open state lock: %w", err)
	}
	// Closing f releases the flock; explicit LOCK_UN would race the defer.
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return contendedLockError(lockPath, f)
		}
		return fmt.Errorf("acquire state lock: %w", err)
	}
	_ = writeLockOwnerPID(f) // best-effort; failure only degrades the next contention error.
	return fn()
}

func writeLockOwnerPID(f *os.File) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek lock file: %w", err)
	}
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate lock file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return fmt.Errorf("write lock owner pid: %w", err)
	}
	return nil
}

func contendedLockError(lockPath string, f *os.File) error {
	if pid, ok := readLockOwnerPID(f); ok {
		if processAlive(pid) {
			return fmt.Errorf("state lock %s held by fleetnode pid=%d; stop it (kill %d) or use a different --state-dir", lockPath, pid, pid)
		}
		// Dead owner + held lock means a subprocess inherited the FD.
		return fmt.Errorf("state lock %s contended; recorded owner pid=%d is not running but the lock is still held (likely a subprocess inherited the FD); kill any lingering fleetnode children or use a different --state-dir", lockPath, pid)
	}
	return fmt.Errorf("state lock %s held by an unknown process; check `pgrep -lf fleetnode` and stop it, or use a different --state-dir", lockPath)
}

func readLockOwnerPID(f *os.File) (int, bool) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, false
	}
	data, err := io.ReadAll(io.LimitReader(f, 32))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	// EPERM means alive but different uid -- still "alive" for our purposes.
	return err == nil || errors.Is(err, syscall.EPERM)
}

// syncDir fsyncs a directory so a preceding os.Rename survives a power loss.
// POSIX only; Windows directory handles don't support FlushFileBuffers.
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
