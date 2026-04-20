package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

const (
	seedDevicePrefix = "seed-device-"

	// Number of columns in the device_metrics INSERT.
	columnsPerRow = 23

	// Continuous aggregate names to refresh after insertion.
	aggregateMetricsHourly = "device_metrics_hourly"
	aggregateMetricsDaily  = "device_metrics_daily"
	aggregateStatusHourly  = "device_status_hourly"
	aggregateStatusDaily   = "device_status_daily"

	// PostgreSQL extended protocol limits queries to math.MaxUint16 parameters.
	maxRowsPerBatch = math.MaxUint16 / columnsPerRow

	// Progress logging interval.
	logEveryNBatches = 10
)

var continuousAggregates = []string{
	aggregateMetricsHourly,
	aggregateMetricsDaily,
	aggregateStatusHourly,
	aggregateStatusDaily,
}

func clearSeedData(ctx context.Context, db *sql.DB) (int64, error) {
	result, err := db.ExecContext(ctx,
		"DELETE FROM device_metrics WHERE device_identifier LIKE $1",
		seedDevicePrefix+"%",
	)
	if err != nil {
		return 0, fmt.Errorf("deleting seed data: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting deleted row count: %w", err)
	}
	return n, nil
}

func findSeedDataBounds(ctx context.Context, db *sql.DB) (time.Time, time.Time, bool, error) {
	var minTime sql.NullTime
	var maxTime sql.NullTime
	err := db.QueryRowContext(ctx,
		`SELECT MIN(time), MAX(time)
		 FROM device_metrics
		 WHERE device_identifier LIKE $1`,
		seedDevicePrefix+"%",
	).Scan(&minTime, &maxTime)
	if err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("querying seed data bounds: %w", err)
	}
	if !minTime.Valid || !maxTime.Valid {
		return time.Time{}, time.Time{}, false, nil
	}
	return minTime.Time, maxTime.Time, true, nil
}

func cleanupRefreshWindow(start, end time.Time) (time.Time, time.Time) {
	// Extend the refresh range to ensure affected hourly and daily buckets are recomputed.
	return start.Add(-24 * time.Hour), end.Add(24 * time.Hour)
}

func insertBatches(ctx context.Context, db *sql.DB, metrics []deviceMetric, batchSize int) (int64, error) {
	if batchSize > maxRowsPerBatch {
		batchSize = maxRowsPerBatch
	}

	var totalInserted int64
	totalBatches := (len(metrics) + batchSize - 1) / batchSize

	for i := 0; i < len(metrics); i += batchSize {
		end := i + batchSize
		if end > len(metrics) {
			end = len(metrics)
		}
		batch := metrics[i:end]
		batchNum := i/batchSize + 1

		n, err := insertBatch(ctx, db, batch)
		if err != nil {
			return totalInserted, fmt.Errorf("batch %d/%d: %w", batchNum, totalBatches, err)
		}
		totalInserted += n

		if batchNum%logEveryNBatches == 0 || batchNum == totalBatches {
			log.Printf("  batch %d/%d: %d rows inserted (total: %d)",
				batchNum, totalBatches, n, totalInserted)
		}
	}

	return totalInserted, nil
}

func insertBatch(ctx context.Context, db *sql.DB, batch []deviceMetric) (int64, error) {
	if len(batch) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO device_metrics (
		time, device_identifier,
		hash_rate_hs, hash_rate_hs_kind,
		temp_c, temp_c_kind,
		fan_rpm, fan_rpm_kind,
		power_w, power_w_kind,
		efficiency_jh, efficiency_jh_kind,
		voltage_v, voltage_v_kind,
		current_a, current_a_kind,
		inlet_temp_c, outlet_temp_c, ambient_temp_c,
		chip_count, chip_count_kind,
		chip_frequency_mhz, health
	) VALUES `)

	args := make([]any, 0, len(batch)*columnsPerRow)

	for i, m := range batch {
		if i > 0 {
			sb.WriteString(",")
		}
		base := i * columnsPerRow
		sb.WriteString(fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8,
			base+9, base+10, base+11, base+12, base+13, base+14, base+15, base+16,
			base+17, base+18, base+19, base+20, base+21, base+22, base+23,
		))

		args = append(args,
			m.Time, m.DeviceIdentifier,
			m.HashRateHs, m.HashRateHsKind,
			m.TempC, m.TempCKind,
			m.FanRPM, m.FanRPMKind,
			m.PowerW, m.PowerWKind,
			m.EfficiencyJH, m.EfficiencyJHKind,
			m.VoltageV, m.VoltageVKind,
			m.CurrentA, m.CurrentAKind,
			m.InletTempC, m.OutletTempC, m.AmbientTempC,
			m.ChipCount, m.ChipCountKind,
			m.ChipFrequencyMHz, m.Health,
		)
	}

	sb.WriteString(" ON CONFLICT DO NOTHING")

	result, err := db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("executing batch insert: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting inserted row count: %w", err)
	}
	return n, nil
}

func refreshAggregates(ctx context.Context, db *sql.DB, start, end time.Time) error {
	for _, agg := range continuousAggregates {
		log.Printf("  refreshing %s ...", agg)
		_, err := db.ExecContext(ctx,
			fmt.Sprintf("CALL refresh_continuous_aggregate('%s', $1::timestamptz, $2::timestamptz)", agg),
			start, end,
		)
		if err != nil {
			return fmt.Errorf("refreshing %s: %w", agg, err)
		}
	}
	return nil
}
