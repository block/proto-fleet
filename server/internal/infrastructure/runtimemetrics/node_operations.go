package runtimemetrics

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// NodeMetrics records bounded operational outcomes. Callers supply only
// application-defined kind/class/result values, never command or device IDs.
type NodeMetrics struct {
	started  metric.Int64Counter
	outcomes metric.Int64Counter
	inflight metric.Int64UpDownCounter
	duration metric.Float64Histogram
	auth     metric.Int64Counter
	protocol metric.Int64Counter
}

func (p *Provider) NewNodeMetrics() (*NodeMetrics, error) {
	m := p.Meter()
	started, e1 := m.Int64Counter("fleet_node_operation_attempts")
	outcomes, e2 := m.Int64Counter("fleet_node_operation_outcomes")
	inflight, e3 := m.Int64UpDownCounter("fleet_node_operations_inflight")
	duration, e4 := m.Float64Histogram("fleet_node_operation_duration_seconds", metric.WithExplicitBucketBoundaries(.1, .5, 1, 5, 15, 30, 60, 120, 300, 900))
	auth, e5 := m.Int64Counter("fleet_node_auth_outcomes")
	protocol, e6 := m.Int64Counter("fleet_node_protocol_checks")
	if err := errors.Join(e1, e2, e3, e4, e5, e6); err != nil {
		return nil, err
	}
	return &NodeMetrics{started, outcomes, inflight, duration, auth, protocol}, nil
}

func (m *NodeMetrics) Started(ctx context.Context, kind, class string) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("kind", kind), attribute.String("admission_class", class))
	m.started.Add(ctx, 1, attrs)
	m.inflight.Add(ctx, 1, attrs)
}

func (m *NodeMetrics) Finished(ctx context.Context, kind, class, result string, duration time.Duration) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{attribute.String("kind", kind), attribute.String("admission_class", class)}
	m.inflight.Add(ctx, -1, metric.WithAttributes(attrs...))
	opts := metric.WithAttributes(append(attrs, attribute.String("result", result))...)
	m.outcomes.Add(ctx, 1, opts)
	m.duration.Record(ctx, duration.Seconds(), opts)
}

func (m *NodeMetrics) AuthOutcome(ctx context.Context, operation, result string) {
	if m != nil {
		m.auth.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", operation), attribute.String("result", result)))
	}
}

func (m *NodeMetrics) ProtocolCheck(compatible bool) {
	if m == nil {
		return
	}
	result := "compatible"
	if !compatible {
		result = "upgrade_required"
	}
	m.protocol.Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", result)))
}
