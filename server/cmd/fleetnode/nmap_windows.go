//go:build windows

package main

import "os"

// No-op on Windows: ACL validation not implemented. Production installs
// must place the agent and adjacent nmap under an Administrator-only dir
// so the ACL inherits a safe owner (see README Security model).
func checkNmapBinaryOwnership(_ string, _ os.FileInfo) error { return nil }
