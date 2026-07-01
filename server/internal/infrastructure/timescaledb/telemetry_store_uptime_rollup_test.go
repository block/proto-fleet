package timescaledb

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
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
	buildingA := createUptimeTestBuilding(t, db, orgID, siteA, "rollup-building-a")
	buildingB := createUptimeTestBuilding(t, db, orgID, siteB, "rollup-building-b")

	at := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	deviceA := "rollup-current-member-a"
	deviceB := "rollup-current-member-b"
	insertMinerStateSnapshotRow(t, db, at, orgID, siteA, buildingA, deviceA, 3)
	insertMinerStateSnapshotRow(t, db, at, orgID, siteB, buildingB, deviceB, 2)
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
		at             time.Time
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
			insertMinerStateSnapshotRow(t, db, at, orgID, sql.NullInt64{}, sql.NullInt64{}, tt.deviceID, tt.state)
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
	insertDeviceMetricForUptimeRequest(t, db, now, deviceIdentifier)
	insertMinerStateSnapshotRow(t, db, now, orgID, sql.NullInt64{}, sql.NullInt64{}, deviceIdentifier, 3)

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

func TestTelemetryStore_InsertMinerStateSnapshotStampsBuildingID(t *testing.T) {
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
	siteID := createUptimeTestSite(t, db, orgID, "snapshot-stamp-site")
	buildingID := createUptimeTestBuilding(t, db, orgID, siteID, "snapshot-stamp-building")
	device := dbSvc.CreateDevice(orgID, "proto")
	pairDeviceForSnapshot(t, db, device.DatabaseID)
	setDevicePlacementForSnapshot(t, db, device.DatabaseID, siteID, buildingID)
	setDeviceStatusForSnapshot(t, db, device.DatabaseID, sqlc.DeviceStatusEnumACTIVE)

	at := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, store.InsertMinerStateSnapshot(ctx, at))

	var gotSiteID, gotBuildingID sql.NullInt64
	var gotState int16
	err = db.QueryRowContext(ctx, `
		SELECT site_id, building_id, state
		FROM miner_state_snapshots
		WHERE time = $1 AND device_identifier = $2
	`, at, device.ID).Scan(&gotSiteID, &gotBuildingID, &gotState)
	require.NoError(t, err)
	require.True(t, gotSiteID.Valid)
	require.True(t, gotBuildingID.Valid)
	assert.Equal(t, siteID.Int64, gotSiteID.Int64)
	assert.Equal(t, buildingID.Int64, gotBuildingID.Int64)
	assert.Equal(t, int16(3), gotState)
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

func createUptimeTestBuilding(t *testing.T, db *sql.DB, orgID int64, siteID sql.NullInt64, name string) sql.NullInt64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		"INSERT INTO building (org_id, site_id, name) VALUES ($1, $2, $3) RETURNING id",
		orgID, siteID, name,
	).Scan(&id)
	require.NoError(t, err)
	return sql.NullInt64{Int64: id, Valid: true}
}

func insertMinerStateSnapshotRow(t *testing.T, db *sql.DB, at time.Time, orgID int64, siteID, buildingID sql.NullInt64, deviceIdentifier string, state int16) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO miner_state_snapshots (time, org_id, site_id, building_id, device_identifier, state)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (time, device_identifier) DO UPDATE SET
			org_id = EXCLUDED.org_id,
			site_id = EXCLUDED.site_id,
			building_id = EXCLUDED.building_id,
			state = EXCLUDED.state
	`, at, orgID, siteID, buildingID, deviceIdentifier, state)
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
	_, err := db.ExecContext(context.Background(),
		fmt.Sprintf("CALL refresh_continuous_aggregate('%s', $1::timestamptz, $2::timestamptz)", view),
		start, end,
	)
	require.NoError(t, err)
}

func pairDeviceForSnapshot(t *testing.T, db *sql.DB, deviceID int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES ($1, 'PAIRED', CURRENT_TIMESTAMP)
		ON CONFLICT (device_id) DO UPDATE SET pairing_status = 'PAIRED'
	`, deviceID)
	require.NoError(t, err)
}

func setDevicePlacementForSnapshot(t *testing.T, db *sql.DB, deviceID int64, siteID, buildingID sql.NullInt64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		UPDATE device
		SET site_id = $2, building_id = $3
		WHERE id = $1
	`, deviceID, siteID, buildingID)
	require.NoError(t, err)
}

func setDeviceStatusForSnapshot(t *testing.T, db *sql.DB, deviceID int64, status sqlc.DeviceStatusEnum) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO device_status (device_id, status)
		VALUES ($1, $2)
		ON CONFLICT (device_id) DO UPDATE SET status = EXCLUDED.status
	`, deviceID, status)
	require.NoError(t, err)
}
