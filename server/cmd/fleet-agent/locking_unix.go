//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// withStateLock acquires an exclusive flock on <dir>/state.lock for the
// duration of fn. enroll and refresh both wrap their full
// load/handshake/save sequences in this lock so a slower writer cannot
// clobber a newer state.yaml after both refreshes complete server-side.
func withStateLock(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open state lock: %w", err)
	}
	// flock is released by the kernel on close; an explicit LOCK_UN would
	// race with the deferred Close.
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	return fn()
}

// syncDir fsyncs a directory so a preceding os.Rename inside it is durable
// across a kernel/power-loss crash. POSIX-only; Windows handles do not
// support FlushFileBuffers on directories.
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
