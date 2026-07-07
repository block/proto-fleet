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

func TestTelemetryStore_FleetMetricRollupServesSiteBodyAndRawTail(t *testing.T) {
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
	siteA := createUptimeTestSite(t, db, orgID, "fleet-rollup-site-a")
	siteB := createUptimeTestSite(t, db, orgID, "fleet-rollup-site-b")
	deviceA := dbSvc.CreateDevice(orgID, "proto")
	deviceB := dbSvc.CreateDevice(orgID, "proto")
	setFleetRollupTestDeviceSite(t, db, deviceA.DatabaseID, siteA)
	setFleetRollupTestDeviceSite(t, db, deviceB.DatabaseID, siteB)
	t.Cleanup(func() {
		cleanupFleetMetricRollupTestRows(t, db, orgID, deviceA.ID, deviceB.ID)
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
		))
	}

	bodyEnd := start.Add(3 * models.FleetMetricRollupBucketDuration)
	require.NoError(t, store.UpsertFleetMetricRollups(ctx, start, bodyEnd))

	end := start.Add(5*models.FleetMetricRollupBucketDuration - time.Nanosecond)
	slide := models.FleetMetricRollupBucketDuration
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID: orgID,
		DeviceIDs: []models.DeviceIdentifier{
			models.DeviceIdentifier(deviceA.ID),
		},
		DeviceListFromSiteScope: true,
		SiteIDs:                 []int64{siteA.Int64},
		MeasurementTypes:        []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes:        []models.AggregationType{models.AggregationTypeAverage},
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
		assert.Equal(t, 100+float64(i), metric.AggregatedValues[0].Value)
		assert.Equal(t, int32(1), metric.DeviceCount)
	}
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
}

func fleetRollupTestStartTime() time.Time {
	offset := time.Duration(time.Now().UnixNano()%10_000) * models.FleetMetricRollupBucketDuration
	return time.Date(2035, time.January, 1, 0, 0, 0, 0, time.UTC).Add(offset).Truncate(models.FleetMetricRollupBucketDuration)
}
