//go:build windows

package main

import "os"

// PATH fallback disabled: Windows ACLs aren't modeled here, so the
// installer must place nmap.exe in an Administrator-only dir adjacent
// to the agent.
const (
	nmapBinaryName        = "nmap.exe"
	nmapAllowPATHFallback = false
)

func checkNmapBinaryOwnership(_ string, _ os.FileInfo) error { return nil }
