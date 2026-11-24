package telemetry_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestMetricsBatchRecoveryPreservesContainerObservations(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	ctx := t.Context()
	user := dbSvc.CreateSuperAdminUser()
	containerA := dbSvc.CreateDevice(user.OrganizationID, "proto")
	containerB := dbSvc.CreateDevice(user.OrganizationID, "proto")
	_, err := db.ExecContext(ctx, `UPDATE discovered_device SET device_class=2
		WHERE id IN (SELECT discovered_device_id FROM device WHERE id IN ($1, $2))`, containerA.DatabaseID, containerB.DatabaseID)
	require.NoError(t, err)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	service := telemetry.NewTelemetryService(telemetry.Config{}, store, nil, nil, nil, nil)

	t1 := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	t2 := t1.Add(time.Millisecond)
	batch := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: containerA.ID, Timestamp: t1, Container: &sdk.ContainerSnapshot{ObservedAt: t1}},
		{DeviceIdentifier: containerB.ID, Timestamp: t1, Container: &sdk.ContainerSnapshot{
			ObservedAt: t1, Tanks: []sdk.ContainerTank{{Position: 0}},
		}},
		{DeviceIdentifier: containerA.ID, Timestamp: t2, Container: &sdk.ContainerSnapshot{ObservedAt: t2}},
		{DeviceIdentifier: "valid-c", Timestamp: t2},
	}

	flushErr, deviceErrors := telemetry.StoreMetricsBatchForTest(ctx, service, batch)
	require.ErrorContains(t, flushErr, "invalid or duplicate tank selector")
	require.Len(t, deviceErrors, 1)
	require.ErrorContains(t, deviceErrors[models.DeviceIdentifier(containerB.ID)], "invalid or duplicate tank selector")

	rows, err := db.QueryContext(ctx, `SELECT time, metric_semantics FROM device_metrics
		WHERE device_identifier=$1 ORDER BY time`, containerA.ID)
	require.NoError(t, err)
	defer rows.Close()
	var times []time.Time
	for rows.Next() {
		var at time.Time
		var semantics int
		require.NoError(t, rows.Scan(&at, &semantics))
		require.Equal(t, 2, semantics)
		times = append(times, at.UTC())
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []time.Time{t1, t2}, times, "recover the older observation before advancing the container watermark")

	var ordinaryRows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM device_metrics WHERE device_identifier='valid-c'").Scan(&ordinaryRows))
	require.Equal(t, 1, ordinaryRows)
	var watermark time.Time
	require.NoError(t, db.QueryRowContext(ctx, "SELECT observed_at FROM container_inventory WHERE device_id=$1", containerA.DatabaseID).Scan(&watermark))
	require.True(t, watermark.Equal(t2))
}
