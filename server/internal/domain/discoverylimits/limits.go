// Package discoverylimits holds the per-command discovery scan caps, shared so
// the agent (enforcing at execution) and the server (enforcing before dispatch)
// can't drift.
package discoverylimits

const (
	// MaxScanTargets caps IP addresses per discovery command.
	MaxScanTargets = 1024

	// MaxPortsPerIP caps per-IP port fan-out to bound resource use.
	MaxPortsPerIP = 10
)
