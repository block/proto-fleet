package timescaledb

import (
	"context"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	models "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

func recordTelemetryFreshness(ctx context.Context, queries *sqlc.Queries, data []models.DeviceMetrics) error {
	params := sqlc.RecordTelemetryFreshnessBatchParams{}
	now := time.Now()
	for _, sample := range data {
		// Rejected container inventory skips device-level measurement persistence.
		// A successful upload of that snapshot cannot establish committed freshness.
		if sample.Container != nil && !sample.Container.InventoryAccepted {
			continue
		}
		observed := sample.FreshnessObservedAt
		known := sample.HasUsableMeasurement() && !observed.IsZero() && !observed.After(now) && observed.After(time.Unix(0, 0))
		if !known {
			observed = time.Unix(0, 0)
		}
		params.Identifiers = append(params.Identifiers, sample.DeviceIdentifier)
		params.ObservedTimes = append(params.ObservedTimes, observed)
		params.Known = append(params.Known, known)
		params.SourceNodes = append(params.SourceNodes, sample.FreshnessSourceNodeID)
	}
	if len(params.Identifiers) == 0 {
		return nil
	}
	if err := queries.RecordTelemetryFreshnessBatch(ctx, params); err != nil {
		return fmt.Errorf("record committed telemetry freshness: %w", err)
	}
	return nil
}

// recoverableFreshnessWrite keeps watermark and telemetry commits one-way atomic:
// a watermark never survives a telemetry rollback, but a recoverable watermark
// error cannot take down otherwise successful telemetry ingestion.
func (s *TimescaleTelemetryStore) recoverableFreshnessWrite(ctx context.Context, queries *sqlc.Queries, data []models.DeviceMetrics) error {
	if err := queries.CreateTelemetryFreshnessSavepoint(ctx); err != nil {
		return fmt.Errorf("create freshness savepoint: %w", err)
	}
	if err := recordTelemetryFreshness(ctx, queries, data); err != nil {
		if rollbackErr := queries.RollbackTelemetryFreshnessSavepoint(ctx); rollbackErr != nil {
			return fmt.Errorf("recover freshness write (%v): %w", err, rollbackErr)
		}
		s.logger.WarnContext(ctx, "freshness write failed; continuing telemetry transaction",
			"event", "telemetry_freshness_write_failed", "error", err, "batch_size", len(data))
	}
	if err := queries.ReleaseTelemetryFreshnessSavepoint(ctx); err != nil {
		return fmt.Errorf("release freshness savepoint: %w", err)
	}
	return nil
}
