package timescaledb

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHashboardMovesAndReplacementsKeepHardwareIdentity(t *testing.T) {
	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	user := dbSvc.CreateSuperAdminUser()
	device := dbSvc.CreateDevice(user.OrganizationID, "proto")
	_, err := db.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
	require.NoError(t, err)
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	board := func(mac string) sdk.ContainerHashboard {
		return sdk.ContainerHashboard{Position: 1, Occupancy: "occupied", MACAddress: mac, State: sdk.MinerStatusActive, MiningState: "MINING_STATE_MINING", ObservedAt: now, StateObservedAt: now, HashrateHS: new(100e12), MetadataJSON: json.RawMessage(`{"firmwareVersion":"1.2.3"}`)}
	}
	moduleIdentity := "module-a"
	poll := func(at time.Time, first, second sdk.ContainerHashboard) error {
		tx, e := db.BeginTx(t.Context(), nil)
		require.NoError(t, e)
		defer func() { _ = tx.Rollback() }()
		s := &sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{
			{Position: 1, ReportedID: moduleIdentity, Hashboards: []sdk.ContainerHashboard{first}},
			{Position: 2, ReportedID: "module-b", Hashboards: []sdk.ContainerHashboard{second}},
		}}}}
		if e = storeContainerInventory(t.Context(), sqlc.New(tx), device.ID, s); e != nil {
			return e
		}
		return tx.Commit()
	}
	empty := sdk.ContainerHashboard{Position: 1, Occupancy: "empty", State: sdk.MinerStatusUnknown}
	require.NoError(t, poll(now, board("a"), empty))
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT hashboard_id FROM hashboard_identity WHERE kind='mac' AND value='a'").Scan(&id))
	require.NoError(t, poll(now.Add(time.Second), empty, board("a")))
	require.NoError(t, poll(now.Add(2*time.Second), board("b"), board("a")))
	// A delayed poll cannot move A back into its former slot or replace B.
	require.NoError(t, poll(now, board("a"), empty))
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard").Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE hashboard_id=$1", id).Scan(&count))
	require.Equal(t, 2, count)
	var position int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT m.position FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id WHERE i.hashboard_id=$1 AND i.removed_at IS NULL", id).Scan(&position))
	require.Equal(t, 2, position)
	// Reinstallation reuses the original hardware ID, with a new placement interval.
	require.NoError(t, poll(now.Add(3*time.Second), board("a"), empty))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard WHERE id=$1", id).Scan(&count))
	require.Equal(t, 1, count)
	moduleIdentity = "replacement-module"
	require.NoError(t, poll(now.Add(4*time.Second), board("a"), empty))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL", id).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE hashboard_id=$1", id).Scan(&count))
	require.Equal(t, 4, count)
	// Neither an unknown slot nor an identical retry removes the installation.
	unknown := sdk.ContainerHashboard{Position: 1, Occupancy: "unknown", State: sdk.MinerStatusUnknown}
	require.NoError(t, poll(now.Add(5*time.Second), unknown, empty))
	require.NoError(t, poll(now.Add(5*time.Second), unknown, empty))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL", id).Scan(&count))
	require.Equal(t, 1, count)
}

func TestHashboardSourceOrderingAndHistoricalSiteScope(t *testing.T) {
	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	user := dbSvc.CreateSuperAdminUser()
	device := dbSvc.CreateDevice(user.OrganizationID, "proto")
	ctx := t.Context()
	_, err := db.ExecContext(ctx, "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
	require.NoError(t, err)
	var siteA, siteB int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site (org_id,name,slug) VALUES ($1,'A','a') RETURNING id", user.OrganizationID).Scan(&siteA))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site (org_id,name,slug) VALUES ($1,'B','b') RETURNING id", user.OrganizationID).Scan(&siteB))
	_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE id=$2", siteA, device.DatabaseID)
	require.NoError(t, err)
	base := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
	poll := func(offset time.Duration, source time.Time, state sdk.MinerStatus, stateTime ...time.Time) {
		b := sdk.ContainerHashboard{Position: 1, Occupancy: "occupied", MACAddress: "aa", Serial: "serial-a", ObservedAt: source, State: state, StateObservedAt: source, MiningState: "MINING_STATE_MINING", HashrateHS: new(200e12), HealthJSON: json.RawMessage(`{"hashrate":200}`)}
		if len(stateTime) > 0 {
			b.StateObservedAt = stateTime[0]
		}
		s := &sdk.ContainerSnapshot{ObservedAt: base.Add(offset), TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, Hashboards: []sdk.ContainerHashboard{b}}}}}}
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		require.NoError(t, storeContainerInventory(ctx, sqlc.New(tx), device.ID, s))
		require.NoError(t, tx.Commit())
	}
	poll(0, base.Add(-10*time.Second), sdk.MinerStatusActive)
	var moduleID, boardID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM container_module").Scan(&moduleID))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM hashboard").Scan(&boardID))
	reader := fleetinventory.NewService(sqlc.New(db))
	read := func(site int64) *pb.HashboardReading {
		response, err := reader.Hashboards(ctx, user.OrganizationID, &pb.GetModuleHashboardsRequest{ModuleIds: []int64{moduleID}}, []fleetinventory.Scope{{Sites: []int64{site}}})
		require.NoError(t, err)
		require.Len(t, response.Modules[0].Slots, 3)
		return response.Modules[0].Slots[0].Board
	}
	require.NotNil(t, read(siteA).HashrateHs)
	poll(time.Second, time.Time{}, sdk.MinerStatusUnknown)
	require.Nil(t, read(siteA).HashrateHs)
	poll(2*time.Second, base.Add(-2*time.Second), sdk.MinerStatusActive)
	require.NotNil(t, read(siteA).HashrateHs, "missing health must not advance the source watermark")
	poll(3*time.Second, base.Add(time.Second), sdk.MinerStatusError)
	poll(4*time.Second, base.Add(-time.Second), sdk.MinerStatusActive, base.Add(4*time.Second))
	require.Equal(t, "active", read(siteA).State, "current operation is independent of old health")
	require.Equal(t, base.Add(4*time.Second), read(siteA).StateObservedAt.AsTime())
	require.Equal(t, base.Add(3*time.Second), read(siteA).CollectedAt.AsTime(), "current collected_at belongs to its health sample")
	_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE id=$2", siteB, device.DatabaseID)
	require.NoError(t, err)
	poll(5*time.Second, base.Add(time.Second), sdk.MinerStatusActive)
	require.Nil(t, read(siteB).HashrateHs, "cached health from previous site is unavailable")
	require.Equal(t, "unknown", read(siteB).State, "prior site's fault classification is not current evidence")
	request := &pb.ListHashboardHistoryRequest{HashboardId: boardID, StartTime: timestamppb.New(base.Add(-time.Minute)), EndTime: timestamppb.New(base.Add(time.Minute))}
	history, err := reader.HashboardHistory(ctx, user.OrganizationID, request, []fleetinventory.Scope{{Sites: []int64{siteB}}})
	require.NoError(t, err)
	require.Empty(t, history.Samples, "previous site health must not leak")
	poll(6*time.Second, base.Add(6*time.Second), sdk.MinerStatusError, base.Add(time.Second))
	require.NotNil(t, read(siteB).HashrateHs)
	require.Nil(t, read(siteB).Health)
	history, err = reader.HashboardHistory(ctx, user.OrganizationID, request, []fleetinventory.Scope{{Sites: []int64{siteB}}})
	require.NoError(t, err)
	require.Len(t, history.Samples, 1)
	require.Equal(t, "unknown", history.Samples[0].State)
	require.Nil(t, history.Samples[0].StateObservedAt)
	require.Nil(t, history.Samples[0].Health)
	require.NotNil(t, history.Samples[0].HashrateHs)
	var storedState string
	var storedTime sql.NullTime
	require.NoError(t, db.QueryRowContext(ctx, "SELECT state,state_observed_at FROM hashboard_metrics WHERE hashboard_id=$1 AND time=$2", boardID, base.Add(6*time.Second)).Scan(&storedState, &storedTime))
	require.Equal(t, "unknown", storedState)
	require.False(t, storedTime.Valid)
	// Previously stored rows must also be protected at read time.
	_, err = db.ExecContext(ctx, "UPDATE hashboard_metrics SET state='error',state_observed_at=$1,health='{\"thermalState\":\"old fault\"}' WHERE hashboard_id=$2 AND time=$3", base.Add(time.Second), boardID, base.Add(6*time.Second))
	require.NoError(t, err)
	history, err = reader.HashboardHistory(ctx, user.OrganizationID, request, []fleetinventory.Scope{{Sites: []int64{siteB}}})
	require.NoError(t, err)
	require.Equal(t, "unknown", history.Samples[0].State)
	require.Nil(t, history.Samples[0].StateObservedAt)
	require.Nil(t, history.Samples[0].Health)
	poll(7*time.Second, base.Add(7*time.Second), sdk.MinerStatusError)
	history, err = reader.HashboardHistory(ctx, user.OrganizationID, request, []fleetinventory.Scope{{Sites: []int64{siteB}}})
	require.NoError(t, err)
	require.Len(t, history.Samples, 2)
	require.Equal(t, "error", history.Samples[1].State)
	require.Equal(t, base.Add(7*time.Second), history.Samples[1].StateObservedAt.AsTime())
	// Initial discovery still retains asynchronous state in the original site.
	oldHistory, err := reader.HashboardHistory(ctx, user.OrganizationID, request, []fleetinventory.Scope{{Sites: []int64{siteA}}})
	require.NoError(t, err)
	require.NotEmpty(t, oldHistory.Samples)
	require.Equal(t, "active", oldHistory.Samples[0].State)
	unreadable, err := reader.Hashboards(ctx, user.OrganizationID, &pb.GetModuleHashboardsRequest{ModuleIds: []int64{moduleID}}, []fleetinventory.Scope{{Sites: []int64{siteA}}})
	require.NoError(t, err)
	require.Empty(t, unreadable.Modules)
	history, err = reader.HashboardHistory(ctx, user.OrganizationID+999, request, []fleetinventory.Scope{{OrgWide: true}})
	require.Error(t, err)
	require.Nil(t, history)
}

func TestHashboardAliasesAndControllerMoves(t *testing.T) {
	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	user := dbSvc.CreateSuperAdminUser()
	a := dbSvc.CreateDevice(user.OrganizationID, "proto")
	b := dbSvc.CreateDevice(user.OrganizationID, "proto")
	ctx := t.Context()
	_, err := db.ExecContext(ctx, "UPDATE discovered_device SET device_class=2 WHERE id IN (SELECT discovered_device_id FROM device WHERE id IN ($1,$2))", a.DatabaseID, b.DatabaseID)
	require.NoError(t, err)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	poll := func(identifier string, offset time.Duration, mac, serial string) error {
		board := sdk.ContainerHashboard{Position: 1, Occupancy: "occupied", MACAddress: mac, Serial: serial, State: sdk.MinerStatusUnknown}
		if mac == "" && serial == "" {
			board.Occupancy = "empty"
		}
		s := &sdk.ContainerSnapshot{ObservedAt: base.Add(offset), TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, Hashboards: []sdk.ContainerHashboard{board}}}}}}
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		if err := storeContainerInventory(ctx, sqlc.New(tx), identifier, s); err != nil {
			return err
		}
		return tx.Commit()
	}
	require.NoError(t, poll(a.ID, 0, "AA-BB-CC-DD-EE-01", ""))
	require.NoError(t, poll(a.ID, time.Second, "aa:bb:cc:dd:ee:01", "serial-1"))
	require.NoError(t, poll(a.ID, 2*time.Second, "", "serial-1"))
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM hashboard").Scan(&count))
	require.Equal(t, 1, count, "serial enrichment and normalized MAC resolve the same hardware")
	require.NoError(t, poll(a.ID, 3*time.Second, "aa:bb:cc:dd:ee:01", "different-serial"))
	require.NoError(t, poll(b.ID, 3*time.Second, "aa:bb:cc:dd:ee:01", "serial-1"))
	var reason string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT s.unavailable_reason FROM hashboard_slot s JOIN container_module m ON m.id=s.module_id JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 AND s.position=1", b.DatabaseID).Scan(&reason))
	require.Equal(t, sdk.HashboardControllerConflict, reason)

	// Explicit removal allows a prompt move without creating a second board.
	require.NoError(t, poll(a.ID, 4*time.Second, "", ""))
	// A delayed report cannot resurrect a board at a time before its removal.
	require.NoError(t, poll(b.ID, 3*time.Second, "aa:bb:cc:dd:ee:01", "serial-1"))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM hashboard_installation WHERE removed_at IS NULL").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, poll(b.ID, 5*time.Second, "aa:bb:cc:dd:ee:01", ""))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM hashboard").Scan(&count))
	require.Equal(t, 1, count)
	var controller, serial string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT d.device_identifier, i.metadata->>'serial' FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id WHERE i.removed_at IS NULL").Scan(&controller, &serial))
	require.Equal(t, b.ID, controller)
	require.Equal(t, "serial-1", serial)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM hashboard_installation").Scan(&count))
	require.Equal(t, 2, count)
}

func TestMissingContainerDoesNotFailTelemetryReconciliation(t *testing.T) {
	dbSvc := testutil.NewDatabaseService(t, nil)
	user := dbSvc.CreateSuperAdminUser()
	device := dbSvc.CreateDevice(user.OrganizationID, "proto")
	_, err := dbSvc.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=now() WHERE id=$1", device.DatabaseID)
	require.NoError(t, err)
	for _, id := range []string{device.ID, "unknown-controller"} {
		require.NoError(t, storeContainerInventory(t.Context(), sqlc.New(dbSvc.DB), id, &sdk.ContainerSnapshot{ObservedAt: time.Now().UTC()}))
	}
}
