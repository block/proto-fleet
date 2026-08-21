package timescaledb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestTelemetryStore_FleetMetricRollupServesOrgBodyAndRawTail(t *testing.T) {
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
	otherUser := dbSvc.CreateSuperAdminUser2()
	otherOrgID := otherUser.OrganizationID
	siteA := createUptimeTestSite(t, db, orgID, "fleet-rollup-site-a")
	siteB := createUptimeTestSite(t, db, orgID, "fleet-rollup-site-b")
	deviceA := dbSvc.CreateDevice(orgID, "proto")
	deviceB := dbSvc.CreateDevice(orgID, "proto")
	otherDevice := dbSvc.CreateDevice(otherOrgID, "proto")
	setFleetRollupTestDeviceSite(t, db, deviceA.DatabaseID, siteA)
	setFleetRollupTestDeviceSite(t, db, deviceB.DatabaseID, siteB)
	t.Cleanup(func() {
		cleanupFleetMetricRollupTestRows(t, db, orgID, deviceA.ID, deviceB.ID, otherDevice.ID)
	})

	start := fleetRollupTestStartTime()
	for i := range 5 {
		at := start.Add(time.Duration(i)*models.FleetMetricRollupBucketDuration + 10*time.Second)
		require.NoError(t, store.StoreDeviceMetrics(ctx,
			modelsV2.DeviceMetrics{
				DeviceIdentifier: deviceA.ID,
				Timestamp:        at,
				HashrateHS:       &modelsV2.MetricValue{Value: 100 + float64(i)},
			},
			modelsV2.DeviceMetrics{
				DeviceIdentifier: deviceB.ID,
				Timestamp:        at,
				HashrateHS:       &modelsV2.MetricValue{Value: 1000 + float64(i)},
			},
			modelsV2.DeviceMetrics{
				DeviceIdentifier: otherDevice.ID,
				Timestamp:        at,
				HashrateHS:       &modelsV2.MetricValue{Value: 10_000 + float64(i)},
			},
		))
	}

	bodyEnd := start.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, bodyEnd))

	end := start.Add(5*models.FleetMetricRollupBucketDuration - time.Nanosecond)
	slide := models.FleetMetricRollupBucketDuration
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		TimeRange: models.TimeRange{
			StartTime: &start,
			EndTime:   &end,
		},
		SlideInterval: &slide,
	})
	require.NoError(t, err)
	require.Len(t, result.Metrics, 5)

	for i, metric := range result.Metrics {
		assert.Equal(t, models.MeasurementTypeHashrate, metric.MeasurementType)
		assert.True(t, start.Add(time.Duration(i)*models.FleetMetricRollupBucketDuration).Equal(metric.OpenTime))
		require.Len(t, metric.AggregatedValues, 1)
		assert.Equal(t, 1100+2*float64(i), metric.AggregatedValues[0].Value)
		assert.Equal(t, int32(2), metric.DeviceCount)
	}
}

func TestTelemetryStore_FleetMetricRollupProgressAdvancesOnEmptyWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	start := fleetRollupTestStartTime().Add(24 * time.Hour)
	end := start.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, end))
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), "DELETE FROM fleet_metric_rollup_progress")
		require.NoError(t, err)
	})

	latest, err := store.GetLatestFleetMetricRollupBucket(ctx)
	require.NoError(t, err)
	assert.True(t, end.Add(-models.FleetMetricRollupBucketDuration).Equal(latest))
}

func TestTelemetryStore_FleetMetricRollupServesSparseCompletedBody(t *testing.T) {
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
	device := dbSvc.CreateDevice(orgID, "proto")
	t.Cleanup(func() {
		cleanupFleetMetricRollupTestRows(t, db, orgID, device.ID)
	})

	start := fleetRollupTestStartTime().Add(48 * time.Hour)
	require.NoError(t, store.StoreDeviceMetrics(ctx, modelsV2.DeviceMetrics{
		DeviceIdentifier: device.ID,
		Timestamp:        start.Add(10 * time.Second),
		HashrateHS:       &modelsV2.MetricValue{Value: 123},
	}))
	bodyEnd := start.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, bodyEnd))

	_, err = db.ExecContext(context.Background(), "DELETE FROM device_metrics WHERE device_identifier = $1", device.ID)
	require.NoError(t, err)

	end := start.Add(5*models.FleetMetricRollupBucketDuration - time.Nanosecond)
	slide := models.FleetMetricRollupBucketDuration
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})
	require.NoError(t, err)
	require.Len(t, result.Metrics, 1)
	assert.True(t, start.Equal(result.Metrics[0].OpenTime))
	require.Len(t, result.Metrics[0].AggregatedValues, 1)
	assert.Equal(t, float64(123), result.Metrics[0].AggregatedValues[0].Value)
}

func TestTelemetryStore_FleetMetricRollupFallsBackBeforeCoverageStart(t *testing.T) {
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
	device := dbSvc.CreateDevice(orgID, "proto")
	t.Cleanup(func() {
		cleanupFleetMetricRollupTestRows(t, db, orgID, device.ID)
	})

	start := fleetRollupTestStartTime().Add(60 * time.Hour)
	for i := range 5 {
		require.NoError(t, store.StoreDeviceMetrics(ctx, modelsV2.DeviceMetrics{
			DeviceIdentifier: device.ID,
			Timestamp:        start.Add(time.Duration(i)*models.FleetMetricRollupBucketDuration + 10*time.Second),
			HashrateHS:       &modelsV2.MetricValue{Value: 100 + float64(i)},
		}))
	}
	coverageStart := start.Add(10 * models.FleetMetricRollupBucketDuration)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO fleet_metric_rollup_progress (id, earliest_bucket, latest_bucket)
		VALUES (TRUE, $1, $2)
		ON CONFLICT (id) DO UPDATE SET earliest_bucket = EXCLUDED.earliest_bucket, latest_bucket = EXCLUDED.latest_bucket
	`, coverageStart, coverageStart.Add(10*models.FleetMetricRollupBucketDuration))
	require.NoError(t, err)

	end := start.Add(5*models.FleetMetricRollupBucketDuration - time.Nanosecond)
	slide := models.FleetMetricRollupBucketDuration
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})
	require.NoError(t, err)
	require.Len(t, result.Metrics, 5)

	for i, metric := range result.Metrics {
		assert.True(t, start.Add(time.Duration(i)*models.FleetMetricRollupBucketDuration).Equal(metric.OpenTime))
		require.Len(t, metric.AggregatedValues, 1)
		assert.Equal(t, 100+float64(i), metric.AggregatedValues[0].Value)
	}
}

func TestTelemetryStore_FleetMetricRollupProgressResetsSkippedGap(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := NewTelemetryStore(db, DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), "DELETE FROM fleet_metric_rollup_progress")
		require.NoError(t, err)
	})

	start := fleetRollupTestStartTime().Add(84 * time.Hour)
	firstEnd := start.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, firstEnd))

	gapStart := firstEnd.Add(10 * models.FleetMetricRollupBucketDuration)
	gapEnd := gapStart.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, gapStart, gapEnd))

	var earliest, latest time.Time
	require.NoError(t, db.QueryRowContext(
		context.Background(),
		"SELECT earliest_bucket, latest_bucket FROM fleet_metric_rollup_progress WHERE id = TRUE",
	).Scan(&earliest, &latest))
	assert.True(t, gapStart.Equal(earliest), "expected earliest %s, got %s", gapStart, earliest)
	assert.True(t, gapEnd.Add(-models.FleetMetricRollupBucketDuration).Equal(latest), "expected latest %s, got %s", gapEnd.Add(-models.FleetMetricRollupBucketDuration), latest)
}

func TestTelemetryStore_FleetMetricRollupRewriteDeletesStaleKeys(t *testing.T) {
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
	siteA := createUptimeTestSite(t, db, orgID, "fleet-rollup-rewrite-site-a")
	siteB := createUptimeTestSite(t, db, orgID, "fleet-rollup-rewrite-site-b")
	device := dbSvc.CreateDevice(orgID, "proto")
	setFleetRollupTestDeviceSite(t, db, device.DatabaseID, siteA)
	t.Cleanup(func() {
		cleanupFleetMetricRollupTestRows(t, db, orgID, device.ID)
	})

	start := fleetRollupTestStartTime().Add(72 * time.Hour)
	require.NoError(t, store.StoreDeviceMetrics(ctx, modelsV2.DeviceMetrics{
		DeviceIdentifier: device.ID,
		Timestamp:        start.Add(10 * time.Second),
		HashrateHS:       &modelsV2.MetricValue{Value: 500},
	}))
	bodyEnd := start.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, bodyEnd))

	setFleetRollupTestDeviceSite(t, db, device.DatabaseID, siteB)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, bodyEnd))

	end := start.Add(5*models.FleetMetricRollupBucketDuration - time.Nanosecond)
	slide := models.FleetMetricRollupBucketDuration
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})
	require.NoError(t, err)
	require.Len(t, result.Metrics, 1)
	require.Len(t, result.Metrics[0].AggregatedValues, 1)
	assert.Equal(t, float64(500), result.Metrics[0].AggregatedValues[0].Value)
	assert.Equal(t, int32(1), result.Metrics[0].DeviceCount)
}

func setFleetRollupTestDeviceSite(t *testing.T, db *sql.DB, deviceID int64, siteID sql.NullInt64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), "UPDATE device SET site_id = $1 WHERE id = $2", siteID, deviceID)
	require.NoError(t, err)
}

func cleanupFleetMetricRollupTestRows(t *testing.T, db *sql.DB, orgID int64, deviceIdentifiers ...string) {
	t.Helper()
	for _, deviceIdentifier := range deviceIdentifiers {
		_, err := db.ExecContext(context.Background(), "DELETE FROM device_metrics WHERE device_identifier = $1", deviceIdentifier)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(context.Background(), "DELETE FROM fleet_metric_rollup_90s WHERE org_id = $1", orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "DELETE FROM fleet_metric_rollup_progress")
	require.NoError(t, err)
}

func fleetRollupTestStartTime() time.Time {
	offset := time.Duration(time.Now().UnixNano()%10_000) * models.FleetMetricRollupBucketDuration
	return time.Date(2035, time.January, 1, 0, 0, 0, 0, time.UTC).Add(offset).Truncate(models.FleetMetricRollupBucketDuration)
}
