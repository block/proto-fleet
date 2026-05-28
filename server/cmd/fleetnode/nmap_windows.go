//go:build windows

package main

import "os"

const (
	nmapBinaryName = "nmap.exe"
	// PATH fallback disabled on Windows: we can't validate Windows ACLs
	// the same way we validate POSIX uid/mode. A less-privileged-writable
	// PATH entry would let an attacker swap the binary. The installer must
	// place nmap.exe in an Administrator-only dir adjacent to the agent.
	nmapAllowPATHFallback = false
)

// No-op on Windows: Windows ACL validation isn't implemented. Production
// installs rely on the adjacent dir's ACL (Administrator-only) plus the
// disabled PATH fallback above to keep this binary trustworthy.
func checkNmapBinaryOwnership(_ string, _ os.FileInfo) error { return nil }
