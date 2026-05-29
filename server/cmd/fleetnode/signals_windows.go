//go:build windows

package main

import (
	"os"
)

// os.Interrupt is what signal.NotifyContext delivers for Ctrl+C and
// service-stop on Windows; SIGHUP doesn't exist there.
func defaultSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
