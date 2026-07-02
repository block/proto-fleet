package timescaledb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryStore_UptimeCountsUseCurrentMembershipDeviceRollups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	siteA := createUptimeTestSite(t, db, orgID, "rollup-site-a")
	siteB := createUptimeTestSite(t, db, orgID, "rollup-site-b")

	at := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	deviceA := "rollup-current-member-a"
	deviceB := "rollup-current-member-b"
	insertMinerStateSnapshotRow(t, db, at, orgID, siteA, deviceA, 3)
	insertMinerStateSnapshotRow(t, db, at, orgID, siteB, deviceB, 2)
	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", at.Add(-time.Minute), at.Add(2*time.Minute))
	deleteMinerStateSnapshotRows(t, db, deviceA, deviceB)

	counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
		OrganizationID: orgID,
		DeviceIDs: []models.DeviceIdentifier{
			models.DeviceIdentifier(deviceB),
			models.DeviceIdentifier(deviceB),
		},
	}, at.Add(-time.Second), at.Add(time.Minute), time.Minute, dataSourceRaw)

	require.Len(t, counts, 1)
	assert.Equal(t, int32(0), counts[0].HashingCount)
	assert.Equal(t, int32(1), counts[0].BrokenCount)
	assert.Equal(t, int32(0), counts[0].NotHashingCount)
}

func TestTelemetryStore_UptimeCountsUseHourlyAndDailyDeviceRollups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	tests := []struct {
		name           string
		view           string
		source         dataSource
		bucketDuration time.Duration
		bucket         time.Time
		deviceID       string
		state          int16
		assertCounts   func(*testing.T, models.UptimeStatusCount)
	}{
		{
			name:           "hourly",
			view:           "miner_state_snapshot_device_hourly",
			source:         dataSourceHourly,
			bucketDuration: time.Hour,
			bucket:         time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour),
			deviceID:       "rollup-hourly-device",
			state:          0,
			assertCounts: func(t *testing.T, count models.UptimeStatusCount) {
				assert.Equal(t, int32(0), count.HashingCount)
				assert.Equal(t, int32(0), count.BrokenCount)
				assert.Equal(t, int32(1), count.NotHashingCount)
			},
		},
		{
			name:           "daily",
			view:           "miner_state_snapshot_device_daily",
			source:         dataSourceDaily,
			bucketDuration: 24 * time.Hour,
			bucket:         time.Now().UTC().Add(-48 * time.Hour).Truncate(24 * time.Hour),
			deviceID:       "rollup-daily-device",
			state:          3,
			assertCounts: func(t *testing.T, count models.UptimeStatusCount) {
				assert.Equal(t, int32(1), count.HashingCount)
				assert.Equal(t, int32(0), count.BrokenCount)
				assert.Equal(t, int32(0), count.NotHashingCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := tt.bucket.Add(5 * time.Minute)
			if tt.bucketDuration == 24*time.Hour {
				at = tt.bucket.Add(2 * time.Hour)
			}
			insertMinerStateSnapshotRow(t, db, at, orgID, sql.NullInt64{}, tt.deviceID, tt.state)
			refreshUptimeDeviceRollup(t, db, tt.view, tt.bucket.Add(-tt.bucketDuration), tt.bucket.Add(tt.bucketDuration))
			deleteMinerStateSnapshotRows(t, db, tt.deviceID)

			counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
				OrganizationID: orgID,
				DeviceIDs:      []models.DeviceIdentifier{models.DeviceIdentifier(tt.deviceID)},
			}, tt.bucket, tt.bucket, tt.bucketDuration, tt.source)

			require.Len(t, counts, 1)
			assert.True(t, tt.bucket.Equal(counts[0].Timestamp), "expected bucket %s, got %s", tt.bucket, counts[0].Timestamp)
			tt.assertCounts(t, counts[0])
		})
	}
}

func TestTelemetryStore_UptimeCountsMergeRawTailWhenRollupIsPartial(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := fmt.Sprintf("rollup-tail-device-%d", time.Now().UnixNano())
	first := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	second := first.Add(time.Minute)

	insertMinerStateSnapshotRow(t, db, first, orgID, sql.NullInt64{}, deviceIdentifier, 3)
	insertMinerStateSnapshotRow(t, db, second, orgID, sql.NullInt64{}, deviceIdentifier, 2)
	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", first.Add(-time.Minute), second.Add(-time.Nanosecond))

	counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
		OrganizationID: orgID,
		DeviceIDs:      []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
	}, first, second, time.Minute, dataSourceRaw)

	require.Len(t, counts, 2)
	assert.True(t, first.Equal(counts[0].Timestamp), "expected bucket %s, got %s", first, counts[0].Timestamp)
	assert.Equal(t, int32(1), counts[0].HashingCount)
	assert.Equal(t, int32(0), counts[0].BrokenCount)
	assert.True(t, second.Equal(counts[1].Timestamp), "expected bucket %s, got %s", second, counts[1].Timestamp)
	assert.Equal(t, int32(0), counts[1].HashingCount)
	assert.Equal(t, int32(1), counts[1].BrokenCount)
}

func TestTelemetryStore_UptimeCountsRecomputePartiallyMaterializedTailBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := fmt.Sprintf("rollup-partial-bucket-device-%d", time.Now().UnixNano())

	// Arrange: a 90s bucket spans two rollup minutes; only the first minute is
	// materialized, so the rollup-built bucket carries the stale hashing state.
	// The device turns broken in the unmaterialized minute and again later in
	// the raw-only tail bucket.
	bucketStart := time.Now().UTC().Add(-2 * time.Hour).Truncate(90 * time.Second)
	materialized := bucketStart
	unmaterialized := bucketStart.Add(time.Minute)
	tailBucket := bucketStart.Add(90 * time.Second)
	inTail := bucketStart.Add(150 * time.Second)
	insertMinerStateSnapshotRow(t, db, materialized, orgID, sql.NullInt64{}, deviceIdentifier, 3)
	insertMinerStateSnapshotRow(t, db, unmaterialized, orgID, sql.NullInt64{}, deviceIdentifier, 2)
	insertMinerStateSnapshotRow(t, db, inTail, orgID, sql.NullInt64{}, deviceIdentifier, 2)
	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", bucketStart.Add(-time.Minute), unmaterialized.Add(-time.Nanosecond))

	// Act
	counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
		OrganizationID: orgID,
		DeviceIDs:      []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
	}, bucketStart, inTail, 90*time.Second, dataSourceRaw)

	// Assert: the overlapping bucket is recomputed from raw, so the state
	// change in the unmaterialized minute wins over the stale rollup count
	require.Len(t, counts, 2)
	assert.True(t, bucketStart.Equal(counts[0].Timestamp), "expected bucket %s, got %s", bucketStart, counts[0].Timestamp)
	assert.Equal(t, int32(0), counts[0].HashingCount)
	assert.Equal(t, int32(1), counts[0].BrokenCount)
	assert.True(t, tailBucket.Equal(counts[1].Timestamp), "expected bucket %s, got %s", tailBucket, counts[1].Timestamp)
	assert.Equal(t, int32(1), counts[1].BrokenCount)
}

func TestTelemetryStore_UptimeRollup1mMatchesRawBucketingForNinetySecondBuckets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := fmt.Sprintf("rollup-90s-device-%d", time.Now().UnixNano())
	at := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute).Add(50 * time.Second)
	start := at.Add(-time.Minute)
	end := at.Add(time.Minute)

	insertMinerStateSnapshotRow(t, db, at, orgID, sql.NullInt64{}, deviceIdentifier, 3)
	rawCounts := store.getUptimeStatusCountsFromSnapshots(ctx, orgID, []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)}, start, end, 90*time.Second)
	require.Len(t, rawCounts, 1)

	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", start.Add(-time.Minute), end.Add(time.Minute))
	rollupCounts := store.getUptimeStatusCountsFromDeviceRollups(ctx, orgID, []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)}, start, end, 90*time.Second, dataSourceRaw)
	require.Len(t, rollupCounts, 1)

	assert.Equal(t, rawCounts[0].Timestamp, rollupCounts[0].Timestamp)
	assert.Equal(t, rawCounts[0].HashingCount, rollupCounts[0].HashingCount)
	assert.Equal(t, rawCounts[0].BrokenCount, rollupCounts[0].BrokenCount)
	assert.Equal(t, rawCounts[0].NotHashingCount, rollupCounts[0].NotHashingCount)
}

func TestTelemetryStore_UptimeRollup1mPicksLatestStateWhenBucketSpansMinutes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := fmt.Sprintf("rollup-latest-state-device-%d", time.Now().UnixNano())

	// Two snapshots one minute apart inside a single 90s bucket: the bucket
	// count must reflect the later state, matching the raw path.
	bucketStart := time.Now().UTC().Add(-2 * time.Hour).Truncate(90 * time.Second)
	first := bucketStart
	second := bucketStart.Add(time.Minute)

	// Arrange
	insertMinerStateSnapshotRow(t, db, first, orgID, sql.NullInt64{}, deviceIdentifier, 3)
	insertMinerStateSnapshotRow(t, db, second, orgID, sql.NullInt64{}, deviceIdentifier, 2)
	rawCounts := store.getUptimeStatusCountsFromSnapshots(ctx, orgID, []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)}, bucketStart, second, 90*time.Second)
	require.Len(t, rawCounts, 1)
	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", bucketStart.Add(-time.Minute), second.Add(time.Minute))

	// Act
	rollupCounts := store.getUptimeStatusCountsFromDeviceRollups(ctx, orgID, []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)}, bucketStart, second, 90*time.Second, dataSourceRaw)

	// Assert
	require.Len(t, rollupCounts, 1)
	assert.Equal(t, int32(0), rollupCounts[0].HashingCount)
	assert.Equal(t, int32(1), rollupCounts[0].BrokenCount)
	assert.Equal(t, rawCounts[0].Timestamp, rollupCounts[0].Timestamp)
	assert.Equal(t, rawCounts[0].HashingCount, rollupCounts[0].HashingCount)
	assert.Equal(t, rawCounts[0].BrokenCount, rollupCounts[0].BrokenCount)
	assert.Equal(t, rawCounts[0].NotHashingCount, rollupCounts[0].NotHashingCount)
}

func TestTelemetryStore_UptimeCountsBoundLargeRawFallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := fmt.Sprintf("rollup-fallback-bound-device-%d", time.Now().UnixNano())

	// A snapshot in the head of the range is never refreshed into the rollup,
	// leaving a coverage gap; only the later snapshot is materialized.
	start := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Minute)
	end := start.Add(3 * time.Hour)
	gapped := start.Add(30 * time.Minute)
	covered := start.Add(2 * time.Hour)

	insertMinerStateSnapshotRow(t, db, gapped, orgID, sql.NullInt64{}, deviceIdentifier, 3)
	insertMinerStateSnapshotRow(t, db, covered, orgID, sql.NullInt64{}, deviceIdentifier, 2)
	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", covered.Add(-time.Minute), covered.Add(time.Minute))

	t.Run("all-devices past the range cap returns partial rollup counts", func(t *testing.T) {
		// Act
		counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
			OrganizationID: orgID,
		}, start, end, time.Minute, dataSourceRaw)

		// Assert: only the rollup-covered bucket, not the raw-only gapped one
		require.Len(t, counts, 1)
		assert.True(t, covered.Equal(counts[0].Timestamp), "expected bucket %s, got %s", covered, counts[0].Timestamp)
		assert.Equal(t, int32(1), counts[0].BrokenCount)
	})

	t.Run("all-devices within the range cap still falls back to raw", func(t *testing.T) {
		// Act
		counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
			OrganizationID: orgID,
		}, gapped, gapped.Add(time.Hour), time.Minute, dataSourceRaw)

		// Assert: raw fallback serves the bucket the rollup is missing
		require.Len(t, counts, 1)
		assert.True(t, gapped.Equal(counts[0].Timestamp), "expected bucket %s, got %s", gapped, counts[0].Timestamp)
		assert.Equal(t, int32(1), counts[0].HashingCount)
	})

	t.Run("small device list still falls back to raw", func(t *testing.T) {
		// Act
		counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
			OrganizationID: orgID,
			DeviceIDs:      []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		}, start, end, time.Minute, dataSourceRaw)

		// Assert: raw fallback covers both buckets despite the 3h range
		require.Len(t, counts, 2)
		assert.Equal(t, int32(1), counts[0].HashingCount)
		assert.Equal(t, int32(1), counts[1].BrokenCount)
	})

	t.Run("device list past the row budget returns partial rollup counts", func(t *testing.T) {
		// Arrange: a service-resolved site scope of 3500 devices over 3h
		// estimates 630k scanned rows, past the 600k budget.
		deviceIDs := make([]models.DeviceIdentifier, 0, 3500)
		deviceIDs = append(deviceIDs, models.DeviceIdentifier(deviceIdentifier))
		for i := 1; i < 3500; i++ {
			deviceIDs = append(deviceIDs, models.DeviceIdentifier(fmt.Sprintf("rollup-fallback-bound-filler-%d", i)))
		}

		// Act
		counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
			OrganizationID: orgID,
			DeviceIDs:      deviceIDs,
		}, start, end, time.Minute, dataSourceRaw)

		// Assert: only the rollup-covered bucket, not the raw-only gapped one
		require.Len(t, counts, 1)
		assert.True(t, covered.Equal(counts[0].Timestamp), "expected bucket %s, got %s", covered, counts[0].Timestamp)
		assert.Equal(t, int32(1), counts[0].BrokenCount)
	})
}

func TestTelemetryStore_UptimeCountsBoundLargeRawTailMerges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := fmt.Sprintf("rollup-tail-bound-device-%d", time.Now().UnixNano())

	// The rollup covers only the first bucket, leaving a ~5h unmaterialized
	// tail with a raw-only snapshot in it; the tail row is never refreshed
	// into the rollup so a merge is detectable.
	start := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Minute)
	end := start.Add(5 * time.Hour)
	tail := start.Add(4 * time.Hour)

	insertMinerStateSnapshotRow(t, db, start, orgID, sql.NullInt64{}, deviceIdentifier, 2)
	insertMinerStateSnapshotRow(t, db, tail, orgID, sql.NullInt64{}, deviceIdentifier, 3)
	refreshUptimeDeviceRollup(t, db, "miner_state_snapshot_device_1m", start.Add(-time.Minute), start.Add(time.Minute))

	t.Run("all-devices tail past the range cap returns rollup counts only", func(t *testing.T) {
		// Act
		counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
			OrganizationID: orgID,
		}, start, end, time.Minute, dataSourceRaw)

		// Assert: only the rollup-covered bucket, not the raw-only tail one
		require.Len(t, counts, 1)
		assert.True(t, start.Equal(counts[0].Timestamp), "expected bucket %s, got %s", start, counts[0].Timestamp)
		assert.Equal(t, int32(1), counts[0].BrokenCount)
	})

	t.Run("small device list with the same tail still merges raw", func(t *testing.T) {
		// Act
		counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
			OrganizationID: orgID,
			DeviceIDs:      []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		}, start, end, time.Minute, dataSourceRaw)

		// Assert: rollup bucket plus the raw tail bucket
		require.Len(t, counts, 2)
		assert.True(t, start.Equal(counts[0].Timestamp), "expected bucket %s, got %s", start, counts[0].Timestamp)
		assert.Equal(t, int32(1), counts[0].BrokenCount)
		assert.True(t, tail.Equal(counts[1].Timestamp), "expected bucket %s, got %s", tail, counts[1].Timestamp)
		assert.Equal(t, int32(1), counts[1].HashingCount)
	})
}

func TestTelemetryStore_GetCombinedMetricsSkipsUptimeCountsWhenNotRequested(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	deviceIdentifier := "skip-uptime-counts-device"
	now := time.Now().UTC().Truncate(time.Minute)
	// The raw metrics query joins device on identifier, org, and lifetime, so
	// the metric needs a device row created before the sample time.
	device := dbSvc.CreateDevice(orgID, "proto")
	_, err = db.ExecContext(ctx,
		`UPDATE discovered_device dd
		 SET device_identifier = $1, created_at = $2
		 FROM device d
		 WHERE d.discovered_device_id = dd.id AND d.id = $3`,
		deviceIdentifier, now.Add(-time.Hour), device.DatabaseID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`UPDATE device SET device_identifier = $1, created_at = $2 WHERE id = $3`,
		deviceIdentifier, now.Add(-time.Hour), device.DatabaseID)
	require.NoError(t, err)
	insertDeviceMetricForUptimeRequest(t, db, now, deviceIdentifier)
	insertMinerStateSnapshotRow(t, db, now, orgID, sql.NullInt64{}, deviceIdentifier, 3)

	start := now.Add(-time.Minute)
	end := now.Add(time.Minute)
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange: models.TimeRange{
			StartTime: &start,
			EndTime:   &end,
		},
		SlideInterval: ptrDuration(time.Minute),
	})

	require.NoError(t, err)
	require.NotEmpty(t, result.Metrics)
	assert.Empty(t, result.UptimeStatusCounts)
}

func createUptimeTestSite(t *testing.T, db *sql.DB, orgID int64, slug string) sql.NullInt64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		"INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id",
		orgID, slug, fmt.Sprintf("%s-%d", slug, time.Now().UnixNano()),
	).Scan(&id)
	require.NoError(t, err)
	return sql.NullInt64{Int64: id, Valid: true}
}

func insertMinerStateSnapshotRow(t *testing.T, db *sql.DB, at time.Time, orgID int64, siteID sql.NullInt64, deviceIdentifier string, state int16) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO miner_state_snapshots (time, org_id, site_id, device_identifier, state)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (time, device_identifier) DO UPDATE SET
			org_id = EXCLUDED.org_id,
			site_id = EXCLUDED.site_id,
			state = EXCLUDED.state
	`, at, orgID, siteID, deviceIdentifier, state)
	require.NoError(t, err)
}

func deleteMinerStateSnapshotRows(t *testing.T, db *sql.DB, deviceIdentifiers ...string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"DELETE FROM miner_state_snapshots WHERE device_identifier = ANY($1)",
		pq.Array(deviceIdentifiers),
	)
	require.NoError(t, err)
}

func insertDeviceMetricForUptimeRequest(t *testing.T, db *sql.DB, at time.Time, deviceIdentifier string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO device_metrics (time, device_identifier, hash_rate_hs)
		VALUES ($1, $2, $3)
		ON CONFLICT (time, device_identifier) DO UPDATE SET
			hash_rate_hs = EXCLUDED.hash_rate_hs
	`, at, deviceIdentifier, 100_000_000.0)
	require.NoError(t, err)
}

func ptrDuration(d time.Duration) *time.Duration {
	return &d
}

func refreshUptimeDeviceRollup(t *testing.T, db *sql.DB, view string, start, end time.Time) {
	t.Helper()
	const maxAttempts = 10
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := db.ExecContext(context.Background(),
			fmt.Sprintf("CALL refresh_continuous_aggregate('%s', $1::timestamptz, $2::timestamptz)", view),
			start, end,
		)
		if err == nil {
			return
		}
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "55P03" || attempt == maxAttempts {
			require.NoError(t, err)
		}
		time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
	}
}
