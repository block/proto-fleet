package runtimemetrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

func (p *Provider) ObserveNodeConnections(count func() int64) error {
	instrument, err := p.Meter().Int64ObservableGauge("fleet_node_control_streams")
	if err != nil {
		return fmt.Errorf("create control stream gauge: %w", err)
	}
	_, err = p.Meter().RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(instrument, count())
		return nil
	}, instrument)
	if err != nil {
		return fmt.Errorf("observe control streams: %w", err)
	}
	return nil
}
