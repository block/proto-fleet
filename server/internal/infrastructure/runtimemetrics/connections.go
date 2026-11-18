package runtimemetrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ConnectionObserver counts physical connection attempts, not pool checkouts.
// Raw connection errors and credentials never become metric attributes.
func (p *Provider) ConnectionObserver(pool string) (func(context.Context, error), error) {
	counter, err := p.Meter().Int64Counter("fleet_db_connection_attempts")
	if err != nil {
		return nil, fmt.Errorf("create connection counter: %w", err)
	}
	success := metric.WithAttributes(attribute.String("pool", pool), attribute.String("result", "success"))
	failure := metric.WithAttributes(attribute.String("pool", pool), attribute.String("result", "failure"))
	counter.Add(context.Background(), 0, success)
	counter.Add(context.Background(), 0, failure)
	return func(ctx context.Context, err error) {
		if err != nil {
			counter.Add(ctx, 1, failure)
		} else {
			counter.Add(ctx, 1, success)
		}
	}, nil
}
