package minerchannel

// Metrics records miner channel operational signals. Default is no-op.
type Metrics interface {
	IncAuditWriteFailure(activityType string)
}

// NoOpMetrics is the default until the platform observability path lands.
type NoOpMetrics struct{}

func (NoOpMetrics) IncAuditWriteFailure(string) {}
