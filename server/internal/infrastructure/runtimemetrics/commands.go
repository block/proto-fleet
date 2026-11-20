package runtimemetrics

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type CommandMetrics struct {
	committed         metric.Int64Counter
	persistenceFailed metric.Int64Counter
	queueLatency      metric.Float64Histogram
	attemptLatency    metric.Float64Histogram
	completionLatency metric.Float64Histogram
}

func (p *Provider) NewCommandMetrics() (*CommandMetrics, error) {
	m := p.Meter()
	committed, e1 := m.Int64Counter("fleet_command_committed_outcomes")
	persistence, e2 := m.Int64Counter("fleet_command_persistence_failures")
	boundaries := metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 900, 1800)
	queue, e3 := m.Float64Histogram("fleet_command_queue_wait_seconds", boundaries)
	attempt, e4 := m.Float64Histogram("fleet_command_attempt_duration_seconds", boundaries)
	completion, e5 := m.Float64Histogram("fleet_command_completion_seconds", boundaries)
	if err := errors.Join(e1, e2, e3, e4, e5); err != nil {
		return nil, err
	}
	return &CommandMetrics{committed, persistence, queue, attempt, completion}, nil
}

func commandAttributes(kind, class string) []attribute.KeyValue {
	return []attribute.KeyValue{attribute.String("kind", kind), attribute.String("command_class", class)}
}

func (m *CommandMetrics) CommandStarted(ctx context.Context, kind, class string, queuedAt time.Time) {
	attrs := metric.WithAttributes(commandAttributes(kind, class)...)
	if age := time.Since(queuedAt); !queuedAt.IsZero() && age >= 0 {
		m.queueLatency.Record(ctx, age.Seconds(), attrs)
	}
}

func (m *CommandMetrics) CommandAttemptFinished(ctx context.Context, kind, class, result string, duration time.Duration) {
	attrs := metric.WithAttributes(append(commandAttributes(kind, class), attribute.String("result", result))...)
	m.attemptLatency.Record(ctx, duration.Seconds(), attrs)
}

func (m *CommandMetrics) CommandCommitted(ctx context.Context, kind, class, result string, queuedAt time.Time) {
	attrs := metric.WithAttributes(append(commandAttributes(kind, class), attribute.String("result", result))...)
	m.committed.Add(ctx, 1, attrs)
	if age := time.Since(queuedAt); !queuedAt.IsZero() && age >= 0 {
		m.completionLatency.Record(ctx, age.Seconds(), attrs)
	}
}

func (m *CommandMetrics) CommandPersistenceFailed(ctx context.Context, class string) {
	m.persistenceFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("command_class", class)))
}
