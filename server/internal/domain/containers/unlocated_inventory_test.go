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

	// A later poll with older source health must not replace a known fault
	// with a false "no issues" result while the old sample is still fresh.
	snapshot.ObservedAt = now.Add(time.Second)
	snapshot.UnlocatedHashboards[0].ObservedAt = now.Add(-time.Second)
	olderHealth, err := json.Marshal(map[string]any{"timestamp": now.Add(-time.Second), "errors": []any{}, "warnings": []any{}})
	require.NoError(t, err)
	snapshot.UnlocatedHashboards[0].HealthJSON = olderHealth
	write()
	regressed, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.False(t, regressed.UnlocatedBoards[0].IssuesKnown)
	require.Nil(t, regressed.UnlocatedBoards[0].HashrateHs)

	snapshot.ObservedAt = now.Add(2 * time.Second)
	snapshot.UnlocatedHashboards[0] = unlocated
	snapshot.UnlocatedHashboards[0].ObservedAt = snapshot.ObservedAt
	snapshot.UnlocatedHashboards[0].HealthJSON, err = json.Marshal(map[string]any{"timestamp": snapshot.ObservedAt, "errors": []any{}, "warnings": []any{}})
	require.NoError(t, err)
	write()
	recovered, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.True(t, recovered.UnlocatedBoards[0].IssuesKnown)
	require.Empty(t, recovered.UnlocatedBoards[0].Issues)

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

	// A site move must withdraw the previous site's evidence from the new reader.
	var siteID int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site(org_id,name,slug) VALUES($1,'New site','new-site') RETURNING id", org).Scan(&siteID))
	_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE device_identifier=$2", siteID, d.DeviceIdentifier)
	require.NoError(t, err)
	newSiteReader := func(site *int64) (bool, error) { return site != nil && *site == siteID, nil }
	moved, err := service.Get(ctx, org, d.DeviceIdentifier, newSiteReader)
	require.NoError(t, err)
	require.Empty(t, moved.UnlocatedBoards)
	// A delayed poll from before the move cannot restore the withdrawn data.
	write()
	moved, err = service.Get(ctx, org, d.DeviceIdentifier, newSiteReader)
	require.NoError(t, err)
	require.Empty(t, moved.UnlocatedBoards)
	// A new observation can populate the new site's inventory.
	snapshot.ObservedAt = time.Now().UTC().Add(time.Second)
	for i := range snapshot.UnlocatedHashboards {
		snapshot.UnlocatedHashboards[i].ObservedAt = snapshot.ObservedAt
	}
	write()
	moved, err = service.Get(ctx, org, d.DeviceIdentifier, newSiteReader)
	require.NoError(t, err)
	require.Len(t, moved.UnlocatedBoards, 2)

	// A later complete map moves the same identities into real board slots.
	snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Second)
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

func TestUnlocatedInventoryRetainedAcrossPartialObservation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "unlocated-retain", Name: "Unlocated retain"})
	require.NoError(t, err)
	d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{
		DeviceIdentifier: "retain-ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		SerialNumber: "retain-serial", Model: "CT1", Manufacturer: "Proto", DriverName: "proto",
		IpAddress: "192.0.2.13", Port: "8080", UrlScheme: "http",
	}, OrgID: org, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: d.DeviceIdentifier}, d)
	require.NoError(t, err)
	devices := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, d.DeviceIdentifier))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, "PAIRED"))
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	service := containers.NewService(sqlstores.NewSQLContainerStore(db))
	allow := func(*int64) (bool, error) { return true, nil }

	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	board := func(serial, mac string, observed time.Time) sdk.ContainerHashboard {
		return sdk.ContainerHashboard{Serial: serial, MACAddress: mac, Occupancy: "occupied", State: sdk.MinerStatusActive,
			ObservedAt: observed, StateObservedAt: observed, HashrateHS: new(100e12), TemperatureC: new(65.0)}
	}
	// enumerated marks a poll that saw every tank: the plugin sets
	// UnlocatedInventoryComplete and (when unlocated boards are present) clears
	// TopologyComplete, mirroring health.go. A partial poll leaves both false.
	writeAt := func(at time.Time, enumerated bool, unlocated []sdk.ContainerHashboard, tanks []sdk.ContainerTank) {
		t.Helper()
		snap := &sdk.ContainerSnapshot{
			ObservedAt: at, Tanks: tanks, UnlocatedHashboards: unlocated,
			UnlocatedInventoryComplete: enumerated,
			TopologyComplete:           enumerated && len(unlocated) == 0,
		}
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: at, Container: snap}))
	}

	// A complete inventory records two placement-free boards.
	writeAt(base, true,
		[]sdk.ContainerHashboard{board("retain-a", "02:00:00:00:00:0a", base), board("retain-b", "02:00:00:00:00:0b", base)}, nil)
	got, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Len(t, got.UnlocatedBoards, 2)

	// A partial poll that reports neither board must not erase the retained inventory.
	writeAt(base.Add(time.Second), false, nil, nil)
	retained, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Len(t, retained.UnlocatedBoards, 2)
	require.ElementsMatch(t, []string{"retain-a", "retain-b"},
		[]string{retained.UnlocatedBoards[0].Serial, retained.UnlocatedBoards[1].Serial})

	// A partial poll that confirms one board in a physical slot drops only that board.
	at := base.Add(2 * time.Second)
	placed := board("retain-a", "02:00:00:00:00:0a", at)
	placed.Position = 1
	writeAt(at, false, nil, []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{
		Position: 1, ReportedID: "module-1", State: sdk.MinerStatusActive, ObservedAt: at,
		Hashboards: []sdk.ContainerHashboard{placed},
	}}}})
	dropped, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Len(t, dropped.UnlocatedBoards, 1)
	require.Equal(t, "retain-b", dropped.UnlocatedBoards[0].Serial)

	// A complete inventory without placement-free boards authoritatively clears them.
	writeAt(base.Add(3*time.Second), true, nil, nil)
	cleared, err := service.Get(ctx, org, d.DeviceIdentifier, allow)
	require.NoError(t, err)
	require.Empty(t, cleared.UnlocatedBoards)
}
