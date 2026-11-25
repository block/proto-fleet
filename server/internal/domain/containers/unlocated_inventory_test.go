package containers_test

import (
	"encoding/json"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestUnlocatedInventoryPersistsAndClearsWithoutInventingPlacements(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "unlocated-inventory", Name: "Unlocated inventory"})
	require.NoError(t, err)
	d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{
		DeviceIdentifier: "unlocated-ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		SerialNumber: "container-serial", Model: "CT1", Manufacturer: "Proto", DriverName: "proto",
		IpAddress: "192.0.2.12", Port: "8080", UrlScheme: "http",
	}, OrgID: org, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: d.DeviceIdentifier}, d)
	require.NoError(t, err)
	devices := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, d.DeviceIdentifier))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, "PAIRED"))
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	health, err := json.Marshal(map[string]any{"timestamp": now, "errors": []map[string]string{{"cause": "HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE"}}})
	require.NoError(t, err)
	board := func(serial string, position int32) sdk.ContainerHashboard {
		return sdk.ContainerHashboard{Serial: serial, Position: position, Occupancy: "occupied", State: sdk.MinerStatusActive,
			ObservedAt: now, StateObservedAt: now, HashrateHS: new(100e12), TemperatureC: new(65.0)}
	}
	unlocated := board("unlocated-1", 0)
	unlocated.MACAddress = "02:00:00:00:00:01"
	unlocated.MetadataJSON = json.RawMessage(`{"firmwareVersion":"2.3.4"}`)
	unlocated.HealthJSON = health
	snapshot := &sdk.ContainerSnapshot{
		ObservedAt: now, TopologyComplete: true,
		Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{
			Position: 1, ReportedID: "module-1", State: sdk.MinerStatusActive, ObservedAt: now,
			Hashboards: []sdk.ContainerHashboard{board("located-1", 1)},
		}}}},
		UnlocatedHashboards: []sdk.ContainerHashboard{unlocated, board("unlocated-2", 0)},
	}
	write := func() {
		t.Helper()
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: snapshot.ObservedAt, Container: snapshot}))
	}
	write()
	service := containers.NewService(sqlstores.NewSQLContainerStore(db))
	allow := func(*int64) (bool, error) { return true, nil }
	got, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Len(t, got.Tanks, 1)
	require.Len(t, got.UnlocatedBoards, 2)
	require.Equal(t, "unlocated-1", got.UnlocatedBoards[0].Serial)
	require.Equal(t, unlocated.MACAddress, got.UnlocatedBoards[0].MacAddress)
	require.Equal(t, "2.3.4", got.UnlocatedBoards[0].FirmwareVersion)
	require.Equal(t, new(100e12), got.UnlocatedBoards[0].HashrateHs)
	require.True(t, got.UnlocatedBoards[0].IssuesKnown)
	require.Equal(t, []string{"board over voltage"}, got.UnlocatedBoards[0].Issues)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NULL").Scan(&count))
	require.Equal(t, 1, count, "unlocated boards must not get an installation")

	// Access checks must apply to inventory identities and diagnostic details.
	denied, err := service.List(ctx, org+1, nil, false, allow)
	require.NoError(t, err)
	require.Empty(t, denied)
	deny := func(*int64) (bool, error) { return false, nil }
	denied, err = service.List(ctx, org, nil, false, deny)
	require.NoError(t, err)
	require.Empty(t, denied)
	_, err = service.Get(ctx, org, d.DeviceIdentifier, deny)
	require.True(t, fleeterror.IsNotFoundError(err))

	_, err = db.ExecContext(ctx, "UPDATE container_inventory SET observed_at=$1", now.Add(-3*time.Minute))
	require.NoError(t, err)
	stale, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Len(t, stale.UnlocatedBoards, 2)
	require.Equal(t, "unlocated-1", stale.UnlocatedBoards[0].Serial)
	require.True(t, stale.UnlocatedBoards[0].Stale)
	require.Nil(t, stale.UnlocatedBoards[0].HashrateHs)
	require.Nil(t, stale.UnlocatedBoards[0].TemperatureC)
	require.False(t, stale.UnlocatedBoards[0].IssuesKnown)
	require.Empty(t, stale.UnlocatedBoards[0].Issues)

	// A later complete map moves the same identities into real board slots.
	snapshot.ObservedAt = now.Add(time.Second)
	mapped := []sdk.ContainerHashboard{board("located-1", 1)}
	for index, entry := range snapshot.UnlocatedHashboards {
		entry.Position = int32(index + 2) //nolint:gosec // The fixture contains two unlocated boards.
		entry.ObservedAt = snapshot.ObservedAt
		entry.StateObservedAt = snapshot.ObservedAt
		mapped = append(mapped, entry)
	}
	snapshot.Tanks[0].Modules[0].Hashboards = mapped
	snapshot.UnlocatedHashboards = nil
	snapshot.TopologyComplete = true
	write()
	resolved, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Empty(t, resolved.UnlocatedBoards)
	require.EqualValues(t, 0, resolved.GetUnlocatedHashboards())
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NULL").Scan(&count))
	require.Equal(t, 3, count)
}
