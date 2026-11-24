package runtimemetrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (p *Provider) DispatchObserver() (func(context.Context, string, string, string, time.Duration), error) {
	if p.provider == nil {
		return nil, nil
	}
	latency, err := p.Meter().Float64Histogram("fleet_command_dispatch_send_seconds", metric.WithExplicitBucketBoundaries(.01, .1, .5, 1, 5, 15, 30, 60, 120, 300))
	if err != nil {
		return nil, fmt.Errorf("create dispatch send histogram: %w", err)
	}
	return func(ctx context.Context, kind, class, result string, duration time.Duration) {
		attrs := metric.WithAttributes(attribute.String("kind", kind), attribute.String("admission_class", class), attribute.String("result", result))
		latency.Record(ctx, duration.Seconds(), attrs)
	}, nil
}
