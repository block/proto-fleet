package runtimepolicy

import "time"

const (
	CommandTransactionBound = 5 * time.Second
	// Bound container status/power reads even when the caller omits a deadline.
	ContainerMiningReadTimeout = 30 * time.Second
	MinerWebRequestTimeout     = 30 * time.Second
	// Relay admission, delivery and response wait have their own server budget.
	// This is not an estimate of when remote execution has stopped.
	MinerWebResponseTimeout = 2 * MinerWebRequestTimeout
)
