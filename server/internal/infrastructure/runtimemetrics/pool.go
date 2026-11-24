package runtimemetrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ObservePool reads sql.DB's in-memory counters. It never acquires a connection
// or executes SQL. Pool names must be a fixed application-defined set.
func (p *Provider) ObservePool(name string, stats func() sql.DBStats) error {
	m := p.Meter()
	open, e1 := m.Int64ObservableGauge("fleet_db_pool_open_connections")
	inUse, e2 := m.Int64ObservableGauge("fleet_db_pool_in_use_connections")
	idle, e3 := m.Int64ObservableGauge("fleet_db_pool_idle_connections")
	limit, e4 := m.Int64ObservableGauge("fleet_db_pool_max_connections")
	waits, e5 := m.Int64ObservableCounter("fleet_db_pool_waits")
	waitTime, e6 := m.Float64ObservableCounter("fleet_db_pool_wait_seconds")
	if err := errors.Join(e1, e2, e3, e4, e5, e6); err != nil {
		return err
	}
	labels := metric.WithAttributes(attribute.String("pool", name))
	_, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := stats()
		o.ObserveInt64(open, int64(s.OpenConnections), labels)
		o.ObserveInt64(inUse, int64(s.InUse), labels)
		o.ObserveInt64(idle, int64(s.Idle), labels)
		// Zero means unlimited. Consumers must exclude it from utilization ratios.
		o.ObserveInt64(limit, int64(s.MaxOpenConnections), labels)
		o.ObserveInt64(waits, s.WaitCount, labels)
		o.ObserveFloat64(waitTime, s.WaitDuration.Seconds(), labels)
		return nil
	}, open, inUse, idle, limit, waits, waitTime)
	if err != nil {
		return fmt.Errorf("register pool metrics: %w", err)
	}
	return nil
}
