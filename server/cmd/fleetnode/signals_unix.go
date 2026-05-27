//go:build !windows

package main

import (
	"os"
	"syscall"
)

// defaultSignals returns the OS signals that should drive an orderly daemon
// shutdown on Unix. SIGHUP catches terminal-close so plugin children get the
// same orderly shutdown as Ctrl+C instead of being orphaned.
func defaultSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}
