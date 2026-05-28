//go:build !windows

package main

import (
	"os"
	"syscall"
)

// SIGHUP catches terminal-close so plugin children shut down orderly
// instead of being orphaned on TTY close.
func defaultSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}
