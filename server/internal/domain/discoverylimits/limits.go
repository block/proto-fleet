// Package discoverylimits holds the per-command discovery scan caps, shared so
// the agent (enforcing at execution) and the server (enforcing before dispatch)
// can't drift.
package discoverylimits

const (
	// MinIPv4PrefixBits caps the breadth of an individual discovery subnet.
	MinIPv4PrefixBits = 20

	// MaxScanTargets caps IP addresses per Fleet Node discovery command.
	MaxScanTargets = 1 << (32 - MinIPv4PrefixBits)

	// MaxPortsPerIP caps per-IP port fan-out to bound resource use.
	MaxPortsPerIP = 10

	// MaxDiscoveryRanges caps a node's configured ranges. It must match max_items
	// on DiscoveryConfig.ranges in fleetnodegateway.proto.
	MaxDiscoveryRanges = 256

	// MaxPartitionAddresses caps addresses in one server-partitioned discovery command.
	MaxPartitionAddresses = 256

	// MaxPartitionProbes caps address x port probes in one server-partitioned
	// discovery command, and so the reports the server admits for it.
	MaxPartitionProbes = 1024
)
