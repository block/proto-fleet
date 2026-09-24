package containers_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerPersistenceAndMinerSeparation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	orgID, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "containers", Name: "Containers"})
	require.NoError(t, err)
	discovered := sqlstores.NewSQLDiscoveredDeviceStore(db)
	devices := sqlstores.NewSQLDeviceStore(db)
	device := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, SerialNumber: "container-serial", Model: "CT1", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.1", Port: "8080", UrlScheme: "http"}, OrgID: orgID, IsActive: true}
	_, err = discovered.Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: orgID, DeviceIdentifier: "ct1"}, device)
	require.NoError(t, err)
	require.NoError(t, devices.InsertDevice(ctx, &device.Device, orgID, "ct1"))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &device.Device, orgID, "PAIRED"))
	service := containers.NewService(sqlstores.NewSQLContainerStore(db))
	allow := func(*int64) (bool, error) { return true, nil }
	// A paired controller appears before its first successful observation.
	rows, err := service.List(ctx, orgID, []int64{}, false, allow)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Stale)
	require.True(t, rows[0].EmbeddedWebViewAvailable)
	require.Equal(t, "http://192.0.2.1:8080", rows[0].Url)
	var nodeID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO fleet_node (org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES ($1,'node',$2,$3,'CONFIRMED') RETURNING id`, orgID, []byte("container-node"), make([]byte, 32)).Scan(&nodeID))
	_, err = db.ExecContext(ctx, `INSERT INTO fleet_node_device (fleet_node_id,device_id,org_id) SELECT $1,id,org_id FROM device WHERE device_identifier='ct1'`, nodeID)
	require.NoError(t, err)
	remoteRows, err := service.List(ctx, orgID, nil, false, allow)
	require.NoError(t, err)
	require.False(t, remoteRows[0].EmbeddedWebViewAvailable)
	require.Equal(t, "http://192.0.2.1:8080", remoteRows[0].Url)
	_, err = db.ExecContext(ctx, `DELETE FROM fleet_node_device WHERE fleet_node_id=$1`, nodeID)
	require.NoError(t, err)
	for _, status := range []string{"AUTHENTICATION_NEEDED", "DEFAULT_PASSWORD", "PAIRED"} {
		require.NoError(t, devices.UpsertDevicePairing(ctx, &device.Device, orgID, status))
		views, err := service.List(ctx, orgID, nil, false, allow)
		require.NoError(t, err)
		require.Equal(t, status != "AUTHENTICATION_NEEDED", views[0].EmbeddedWebViewAvailable)
		require.Equal(t, status != "AUTHENTICATION_NEEDED", views[0].Url != "")
	}
	_, err = db.ExecContext(ctx, `UPDATE discovered_device SET is_active=false WHERE device_identifier='ct1'`)
	require.NoError(t, err)
	inactive, err := service.List(ctx, orgID, nil, false, allow)
	require.NoError(t, err)
	require.False(t, inactive[0].EmbeddedWebViewAvailable)
	require.Empty(t, inactive[0].Url)
	_, err = db.ExecContext(ctx, `UPDATE discovered_device SET is_active=true WHERE device_identifier='ct1'`)
	require.NoError(t, err)

	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	snapshot := &sdk.ContainerSnapshot{ObservedAt: now, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, State: sdk.ModuleAttention, ObservedAt: now}, {Position: 2, State: sdk.ModuleHashing, ObservedAt: now}}}}}
	pue := 1.05
	snapshot.Plant = &sdk.ContainerPlant{ObservedAt: now, PUE: &pue}
	require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: device.DeviceIdentifier, Timestamp: now, Container: snapshot, PowerW: &telemetry.MetricValue{Value: 9000}}))
	var plantCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM container_plant_metrics").Scan(&plantCount))
	require.Equal(t, 1, plantCount)
	// An older successful response must not overwrite more recent health.
	snapshot.ObservedAt = now.Add(-time.Minute)
	snapshot.Tanks[0].Modules[0].State = sdk.ModuleHashing
	require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: device.DeviceIdentifier, Timestamp: snapshot.ObservedAt, Container: snapshot}))
	rows, err = service.List(ctx, orgID, nil, false, allow)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].Stale)
	require.EqualValues(t, 1, rows[0].Tanks[0].AttentionCount)
	require.EqualValues(t, 1, rows[0].Tanks[0].HashingCount)
	require.Equal(t, 9000.0, rows[0].GetPowerW())

	_, err = db.ExecContext(ctx, `UPDATE container_snapshot SET snapshot=jsonb_set(snapshot, '{tanks,0,modules,1,observed_at}', to_jsonb($1::timestamptz))`, now.Add(-121*time.Second))
	require.NoError(t, err)
	expired, err := service.List(ctx, orgID, nil, false, allow)
	require.NoError(t, err)
	require.False(t, expired[0].Stale, "collection remains recent while source evidence expires")
	require.Zero(t, expired[0].Tanks[0].HashingCount)
	require.EqualValues(t, 1, expired[0].Tanks[0].UnknownCount)
	require.Equal(t, 9000.0, expired[0].GetPowerW())
	// Loss of polling retains topology but never reports it as current health.
	_, err = db.ExecContext(ctx, "UPDATE container_snapshot SET observed_at = $1", now.Add(-10*time.Minute))
	require.NoError(t, err)
	rows, err = service.List(ctx, orgID, nil, false, allow)
	require.NoError(t, err)
	require.True(t, rows[0].Stale)
	require.Len(t, rows[0].Tanks, 1)
	miners, _, _, err := devices.ListMinerStateSnapshots(ctx, orgID, "", 10, nil, nil)
	require.NoError(t, err)
	require.Empty(t, miners)
	ids, err := devices.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = devices.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, &interfaces.MinerFilter{IncludeContainers: true})
	require.NoError(t, err)
	require.Equal(t, []string{"ct1"}, ids)
	count, err := devices.GetTotalPairedMiners(ctx, orgID, nil)
	require.NoError(t, err)
	require.Zero(t, count)
	count, err = devices.GetTotalPairedDevices(ctx, orgID, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	denied, err := service.List(ctx, orgID, nil, false, func(*int64) (bool, error) { return false, nil })
	require.NoError(t, err)
	require.Empty(t, denied)
	otherOrg, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "other", Name: "Other"})
	require.NoError(t, err)
	rows, err = service.List(ctx, otherOrg, nil, false, allow)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = devices.SoftDeleteDevices(ctx, []string{device.DeviceIdentifier}, orgID)
	require.NoError(t, err)
	rows, err = service.List(ctx, orgID, nil, false, allow)
	require.NoError(t, err)
	require.Empty(t, rows)
}
