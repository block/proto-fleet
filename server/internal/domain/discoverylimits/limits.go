// Package discoverylimits holds the per-command discovery scan caps, shared so
// the agent (enforcing at execution) and the server (enforcing before dispatch)
// can't drift.
package discoverylimits

const (
	// MinIPv4PrefixBits caps the breadth of an individual discovery subnet.
	MinIPv4PrefixBits = 22

	// MaxScanTargets caps IP addresses per Fleet Node discovery command.
	MaxScanTargets = 1 << (32 - MinIPv4PrefixBits)

	// MaxPortsPerIP caps per-IP port fan-out to bound resource use.
	MaxPortsPerIP = 10
)
