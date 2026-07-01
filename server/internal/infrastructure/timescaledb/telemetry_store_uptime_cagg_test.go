package timescaledb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryStore_UptimeCountsUseSnapshotCountsCAGG(t *testing.T) {
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
	siteA := createUptimeTestSite(t, db, orgID, "Uptime Site A")
	siteB := createUptimeTestSite(t, db, orgID, "Uptime Site B")
	buildingA := createUptimeTestBuilding(t, db, orgID, siteA, "Uptime Building A")
	buildingB := createUptimeTestBuilding(t, db, orgID, siteB, "Uptime Building B")

	start := time.Now().UTC().Add(-2 * time.Hour).Truncate(2 * time.Minute)
	latest := start.Add(time.Minute)
	insertMinerStateSnapshotRow(t, db, start, orgID, siteA, buildingA, "cagg-site-a-1", 3)
	insertMinerStateSnapshotRow(t, db, start, orgID, siteB, buildingB, "cagg-site-b-1", 0)
	insertMinerStateSnapshotRow(t, db, latest, orgID, siteA, buildingA, "cagg-site-a-1", 2)
	insertMinerStateSnapshotRow(t, db, latest, orgID, siteB, buildingB, "cagg-site-b-1", 1)
	refreshUptimeSnapshotCounts(t, db, start.Add(-time.Minute), latest.Add(time.Minute))

	allCounts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
		OrganizationID: orgID,
	}, start.Add(-time.Second), latest.Add(time.Second), 2*time.Minute)
	require.Len(t, allCounts, 1)
	assert.Equal(t, int32(0), allCounts[0].HashingCount)
	assert.Equal(t, int32(1), allCounts[0].BrokenCount)
	assert.Equal(t, int32(1), allCounts[0].NotHashingCount)

	siteCounts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
		OrganizationID: orgID,
		SiteIDs:        []int64{siteA.Int64},
	}, start.Add(-time.Second), latest.Add(time.Second), 2*time.Minute)
	require.Len(t, siteCounts, 1)
	assert.Equal(t, int32(0), siteCounts[0].HashingCount)
	assert.Equal(t, int32(1), siteCounts[0].BrokenCount)
	assert.Equal(t, int32(0), siteCounts[0].NotHashingCount)
}

func TestTelemetryStore_UptimeCountsExplicitDeviceIDsUseRawSnapshots(t *testing.T) {
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
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	insertMinerStateSnapshotRow(t, db, at, orgID, sql.NullInt64{}, sql.NullInt64{}, "explicit-uptime-device", 3)

	counts := store.uptimeCountsForQuery(ctx, models.CombinedMetricsQuery{
		OrganizationID:    orgID,
		DeviceIDs:         []models.DeviceIdentifier{"explicit-uptime-device", "explicit-uptime-device"},
		ExplicitDeviceIDs: true,
	}, at.Add(-time.Second), at.Add(time.Second), time.Minute)

	require.Len(t, counts, 1)
	assert.Equal(t, int32(1), counts[0].HashingCount)
	assert.Equal(t, int32(0), counts[0].BrokenCount)
	assert.Equal(t, int32(0), counts[0].NotHashingCount)
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
		OrganizationID:    orgID,
		DeviceIDs:         []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		ExplicitDeviceIDs: true,
		MeasurementTypes:  []models.MeasurementType{models.MeasurementTypeHashrate},
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

func TestTelemetryStore_GetCombinedMetricsIncludesUptimeCountsWhenRequested(t *testing.T) {
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
	deviceIdentifier := "include-uptime-counts-device"
	now := time.Now().UTC().Truncate(time.Minute)
	insertDeviceMetricForUptimeRequest(t, db, now, deviceIdentifier)
	insertMinerStateSnapshotRow(t, db, now, orgID, sql.NullInt64{}, sql.NullInt64{}, deviceIdentifier, 3)

	start := now.Add(-time.Minute)
	end := now.Add(time.Minute)
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:    orgID,
		DeviceIDs:         []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		ExplicitDeviceIDs: true,
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeHashrate,
			models.MeasurementTypeUptime,
		},
		TimeRange: models.TimeRange{
			StartTime: &start,
			EndTime:   &end,
		},
		SlideInterval: ptrDuration(time.Minute),
	})

	require.NoError(t, err)
	require.NotEmpty(t, result.Metrics)
	require.Len(t, result.UptimeStatusCounts, 1)
	assert.Equal(t, int32(1), result.UptimeStatusCounts[0].HashingCount)
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
	siteID := createUptimeTestSite(t, db, orgID, "Snapshot Stamp Site")
	buildingID := createUptimeTestBuilding(t, db, orgID, siteID, "Snapshot Stamp Building")
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

func createUptimeTestSite(t *testing.T, db *sql.DB, orgID int64, name string) sql.NullInt64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		"INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id",
		orgID, name, name+"-slug",
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

func refreshUptimeSnapshotCounts(t *testing.T, db *sql.DB, start, end time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"CALL refresh_continuous_aggregate('miner_state_snapshot_counts_1m', $1::timestamptz, $2::timestamptz)",
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
