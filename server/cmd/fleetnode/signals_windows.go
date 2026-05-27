//go:build windows

package main

import (
	"os"
)

// defaultSignals returns the OS signals that should drive an orderly daemon
// shutdown on Windows. SIGHUP doesn't exist there; os.Interrupt is what
// signal.NotifyContext delivers for Ctrl+C and service-stop events.
func defaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
