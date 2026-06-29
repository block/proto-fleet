package timescaledb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryStore_UptimeStatusCountsFromHourlyRollups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	orgID := time.Now().UnixNano()
	bucket := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		cleanupMinerStateRollups(t, db, orgID)
	})

	insertMinerStateHourlyRollup(t, db, orgID, bucket, "uptime-rollup-hourly-1", 3)
	insertMinerStateHourlyRollup(t, db, orgID, bucket, "uptime-rollup-hourly-2", 0)
	insertMinerStateHourlyRollup(t, db, orgID, bucket.Add(time.Hour), "uptime-rollup-hourly-1", 2)

	counts := store.getUptimeStatusCountsFromHourlyRollups(ctx, orgID, nil, bucket, bucket.Add(time.Hour))
	require.Len(t, counts, 2)
	assert.Equal(t, int32(1), counts[0].HashingCount)
	assert.Equal(t, int32(1), counts[0].NotHashingCount)
	assert.Equal(t, int32(0), counts[0].BrokenCount)
	assert.Equal(t, int32(0), counts[1].HashingCount)
	assert.Equal(t, int32(0), counts[1].NotHashingCount)
	assert.Equal(t, int32(1), counts[1].BrokenCount)

	filtered := store.getUptimeStatusCountsFromHourlyRollups(
		ctx,
		orgID,
		[]models.DeviceIdentifier{"uptime-rollup-hourly-1"},
		bucket,
		bucket,
	)
	require.Len(t, filtered, 1)
	assert.Equal(t, int32(1), filtered[0].HashingCount)
	assert.Equal(t, int32(0), filtered[0].NotHashingCount)
}

func TestTelemetryStore_UptimeStatusCountsFromDailyRollups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	orgID := time.Now().UnixNano()
	bucket := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		cleanupMinerStateRollups(t, db, orgID)
	})

	insertMinerStateDailyRollup(t, db, orgID, bucket, "uptime-rollup-daily-1", 1)
	insertMinerStateDailyRollup(t, db, orgID, bucket, "uptime-rollup-daily-2", 2)
	insertMinerStateDailyRollup(t, db, orgID, bucket, "uptime-rollup-daily-3", 3)

	counts := store.getUptimeStatusCountsFromDailyRollups(ctx, orgID, nil, bucket, bucket)
	require.Len(t, counts, 1)
	assert.Equal(t, int32(1), counts[0].HashingCount)
	assert.Equal(t, int32(1), counts[0].NotHashingCount)
	assert.Equal(t, int32(1), counts[0].BrokenCount)
}

func insertMinerStateHourlyRollup(t *testing.T, db *sql.DB, orgID int64, bucket time.Time, deviceIdentifier string, state int16) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO miner_state_snapshot_hourly (bucket, sample_time, org_id, device_identifier, state)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (bucket, device_identifier) DO UPDATE SET
			sample_time = EXCLUDED.sample_time,
			org_id = EXCLUDED.org_id,
			state = EXCLUDED.state`,
		bucket, bucket.Add(59*time.Minute), orgID, deviceIdentifier, state)
	require.NoError(t, err)
}

func insertMinerStateDailyRollup(t *testing.T, db *sql.DB, orgID int64, bucket time.Time, deviceIdentifier string, state int16) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO miner_state_snapshot_daily (bucket, sample_time, org_id, device_identifier, state)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (bucket, device_identifier) DO UPDATE SET
			sample_time = EXCLUDED.sample_time,
			org_id = EXCLUDED.org_id,
			state = EXCLUDED.state`,
		bucket, bucket.Add(23*time.Hour), orgID, deviceIdentifier, state)
	require.NoError(t, err)
}

func cleanupMinerStateRollups(t *testing.T, db *sql.DB, orgID int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `DELETE FROM miner_state_snapshot_hourly WHERE org_id = $1`, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), `DELETE FROM miner_state_snapshot_daily WHERE org_id = $1`, orgID)
	require.NoError(t, err)
}
