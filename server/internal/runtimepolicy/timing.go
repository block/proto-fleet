package runtimepolicy

import "time"

const (
	CommandTransactionBound = 5 * time.Second
	// Bound container status/power reads even when the caller omits a deadline.
	ContainerMiningReadTimeout = 30 * time.Second
	// Bound controller log and audit reads: HA selection, the plugin's own
	// read deadlines (5 s for a log tail, 10 s for an audit page) and the
	// Fleet Node round trip. The read holds the container's binding lock, so
	// keep this close to the plugin's deadlines.
	ControllerRecordsReadTimeout = 15 * time.Second
	MinerWebRequestTimeout       = 30 * time.Second
	// Relay admission, delivery and response wait have their own server budget.
	// This is not an estimate of when remote execution has stopped.
	MinerWebResponseTimeout = 2 * MinerWebRequestTimeout
)
