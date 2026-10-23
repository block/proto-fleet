package timescaledb

import (
	"database/sql"
	"strings"
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

func TestPSUModuleHistoryUsesObservedSiteAndSourceTime(t *testing.T) {
	r := newContainerRollupRig(t)
	var org, siteA, siteB int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT org_id FROM device WHERE id=$1", r.databaseID).Scan(&org))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'A','a') RETURNING id", org).Scan(&siteA))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'B','b') RETURNING id", org).Scan(&siteB))
	move := func(site int64) {
		_, err := r.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", site, r.databaseID)
		require.NoError(t, err)
	}
	move(siteA)
	r.base = time.Now().UTC().Truncate(time.Microsecond)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var module int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT module_id FROM container_module_identity WHERE org_id=$1 AND reported_module_id='module-1'", org).Scan(&module))
	move(siteB)
	for _, offset := range []time.Duration{time.Second, 2 * time.Second} {
		s := r.snapshot(offset, false, 110e12, 220e12)
		power := s.Tanks[0].Modules[0].Metrics.Rollup
		power.SourceTimes[power.PowerSource] = r.base
		power.Power.OldestAt, power.Power.NewestAt = r.base, r.base
		sdk.CalculateContainerRollups(s)
		r.store(t, s)
		var watts sql.NullFloat64
		var hash float64
		require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT power_w,hashrate_hs FROM container_module_latest_metrics WHERE module_id=$1", module).Scan(&watts, &hash))
		require.False(t, watts.Valid, "a cached PSU reading cannot acquire the new site's authority")
		require.Equal(t, 110e12, hash, "fresh board telemetry remains available")
	}
	reader := fleetinventory.NewService(sqlc.New(r.db))
	request := &pb.ListModuleHistoryRequest{ModuleId: module, StartTime: timestamppb.New(r.base.Add(-time.Second)), EndTime: timestamppb.New(r.base.Add(time.Minute))}
	oldSite, err := reader.History(t.Context(), org, request, []fleetinventory.Scope{{Sites: []int64{siteA}}})
	require.NoError(t, err, "moving the PSU cannot revoke access to the original site's readings")
	require.Len(t, oldSite.Samples, 1)
	require.Equal(t, 100e12, *oldSite.Samples[0].HashrateHs)
	newSite, err := reader.History(t.Context(), org, request, []fleetinventory.Scope{{Sites: []int64{siteB}}})
	require.NoError(t, err)
	require.Len(t, newSite.Samples, 2)
	for _, sample := range newSite.Samples {
		require.Nil(t, sample.PowerW)
		require.Equal(t, 110e12, *sample.HashrateHs)
	}
}

func TestPSUMoveAcrossControllersPreservesPlacementEvidence(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	rigs := []*modulePSUIdentityRig{}
	for range 2 {
		device := database.CreateDevice(user.OrganizationID, "proto")
		_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
		require.NoError(t, err)
		rigs = append(rigs, &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, device: device.ID, orgID: user.OrganizationID, base: base})
	}
	first, second := rigs[0], rigs[1]
	poll := func(r *modulePSUIdentityRig, offset time.Duration, board string) {
		at := base.Add(offset)
		require.NoError(t, r.store(t, modulePSUSnapshot(at, sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, 1, "psu", identifiedBoard(at, board))}})))
	}
	poll(first, 0, "board-a")
	module := first.moduleIDAt(t, 1, 1)
	board := first.boardID(t, "board-a")
	poll(second, time.Second, "board-b")
	var controller int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT t.device_id FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=$1", module).Scan(&controller))
	require.Equal(t, first.deviceID, controller, "a simultaneous controller claim cannot move the PSU")
	var confirmed bool
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT identity_confirmed FROM container_module WHERE id=$1", module).Scan(&confirmed))
	require.False(t, confirmed, "conflicting controllers withdraw command eligibility")
	// Explicit removal allows a transfer before the competing-controller timeout.
	require.NoError(t, first.store(t, modulePSUSnapshot(base.Add(2*time.Second))))
	poll(second, 3*time.Second, "board-a")
	require.Equal(t, module, second.moduleIDAt(t, 1, 1))
	var oldController, newController int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT device_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NOT NULL", board).Scan(&oldController))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT device_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL", board).Scan(&newController))
	require.Equal(t, first.deviceID, oldController)
	require.Equal(t, second.deviceID, newController)
	// This is newer for the old controller but older than the PSU's latest poll.
	poll(first, 2500*time.Millisecond, "board-a")
	require.Equal(t, module, second.moduleIDAt(t, 1, 1))
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT identity_confirmed FROM container_module WHERE id=$1", module).Scan(&confirmed))
	require.True(t, confirmed, "a delayed old controller must not withdraw a newer identity")
}

func TestModuleMoveAcrossControllersWithReplacementBoard(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	base := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Microsecond)
	var rigs []*modulePSUIdentityRig
	for range 2 {
		device := database.CreateDevice(user.OrganizationID, "proto")
		_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
		require.NoError(t, err)
		rigs = append(rigs, &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, device: device.ID, orgID: user.OrganizationID, base: base})
	}
	first, second := rigs[0], rigs[1]
	poll := func(r *modulePSUIdentityRig, at time.Time, tank, position int32, serial string) {
		t.Helper()
		board := identifiedBoard(at, serial)
		board.HashrateHS = new(100e12)
		require.NoError(t, r.store(t, modulePSUSnapshot(at, sdk.ContainerTank{Position: tank, Modules: []sdk.ContainerModule{confirmedModule(at, position, "module-a", board)}})))
	}
	poll(first, base, 1, 1, "old-board")
	moduleID := first.moduleIDAt(t, 1, 1)
	oldBoardID := first.boardID(t, "old-board")
	var oldInstallationID, oldTankID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT id,tank_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL", oldBoardID).Scan(&oldInstallationID, &oldTankID))

	// The original controller goes silent without reporting an explicit removal.
	// After its claim expires, the same module arrives with a different board.
	movedAt := base.Add(3 * time.Minute)
	poll(second, movedAt, 2, 3, "replacement-board")
	require.Equal(t, moduleID, second.moduleIDAt(t, 2, 3))
	newBoardID := second.boardID(t, "replacement-board")
	require.NotEqual(t, oldBoardID, newBoardID)

	var recordedDevice, recordedTank int64
	var recordedPosition int32
	var removedAt time.Time
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT device_id,tank_id,module_position,removed_at FROM hashboard_installation WHERE id=$1", oldInstallationID).Scan(&recordedDevice, &recordedTank, &recordedPosition, &removedAt))
	require.Equal(t, first.deviceID, recordedDevice, "closing the old installation preserves its controller")
	require.Equal(t, oldTankID, recordedTank)
	require.EqualValues(t, 1, recordedPosition)
	require.True(t, movedAt.Equal(removedAt))

	// A subsequent poll must also commit and reuse the new installation.
	poll(second, movedAt.Add(time.Second), 2, 3, "replacement-board")
	var installedBoard, installedController int64
	var installedAt, lastSeenAt time.Time
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT hashboard_id,device_id,module_position,installed_at,last_seen_at FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL", moduleID).Scan(&installedBoard, &installedController, &recordedPosition, &installedAt, &lastSeenAt))
	require.Equal(t, newBoardID, installedBoard)
	require.Equal(t, second.deviceID, installedController)
	require.EqualValues(t, 3, recordedPosition)
	require.True(t, movedAt.Equal(installedAt))
	require.True(t, movedAt.Add(time.Second).Equal(lastSeenAt))
	var oldSamples, newSamples int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FILTER (WHERE hashboard_id=$1),count(*) FILTER (WHERE hashboard_id=$2) FROM hashboard_metrics", oldBoardID, newBoardID).Scan(&oldSamples, &newSamples))
	require.Equal(t, 1, oldSamples, "the original board retains its telemetry history")
	require.Equal(t, 2, newSamples, "both destination polls persist telemetry")
}

func TestUnresolvedPSUPreservesIndependentBoardTelemetry(t *testing.T) {
	for _, tc := range []struct{ name, psu string }{
		{"missing", ""}, {"duplicate", "duplicate"}, {"invalid NUL", "psu\x00"},
		{"too long", strings.Repeat("p", 257)}, {"padded identity", "\u00a0psu\u00a0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			modules := []sdk.ContainerModule{}
			for _, slot := range []struct {
				position int32
				serial   string
			}{{1, "a"}, {2, "b"}} {
				board := identifiedBoard(r.base, slot.serial)
				board.HashrateHS = new(100e12)
				m := confirmedModule(r.base, slot.position, tc.psu, board)
				m.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: r.base, PowerW: new(1000.0)}
				modules = append(modules, m)
			}
			require.NoError(t, r.store(t, modulePSUSnapshot(r.base, sdk.ContainerTank{Position: 1, Modules: modules})))
			var boards, moduleSamples int
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_metrics WHERE hashrate_hs=100e12").Scan(&boards))
			require.Equal(t, 2, boards)
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_module_metrics").Scan(&moduleSamples))
			require.Zero(t, moduleSamples)
		})
	}
}

func TestPSUPlacementChangeWithdrawsLatestReadings(t *testing.T) {
	for _, change := range []string{"position without metrics", "site before next poll"} {
		t.Run(change, func(t *testing.T) {
			r := newContainerRollupRig(t)
			initial := r.snapshot(0, false, 100e12, 200e12)
			initial.Tanks[0].Modules[0].IdentityConfirmed = true
			r.store(t, initial)
			var module int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT id FROM container_module WHERE reported_id='module-1'").Scan(&module))
			var before []byte
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT source_times FROM container_module_latest_metrics WHERE module_id=$1", module).Scan(&before))
			if change == "position without metrics" {
				at := r.base.Add(time.Second)
				r.store(t, modulePSUSnapshot(at, sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, 2, "module-1")}}))
			} else {
				_, err := r.db.ExecContext(t.Context(), "WITH target AS (INSERT INTO site(org_id,name,slug) SELECT org_id,'new site','new-site' FROM device WHERE id=$1 RETURNING id) UPDATE device SET site_id=(SELECT id FROM target) WHERE id=$1", r.databaseID)
				require.NoError(t, err)
				// Captured before assignment but newer than the previous inventory:
				// it must not restore readings under the new site's authority.
				delayed := r.snapshot(time.Second, false, 999e12, 999e12)
				r.store(t, delayed)
				require.False(t, delayed.InventoryAccepted)
				var confirmed bool
				var state string
				require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT identity_confirmed,state FROM container_module WHERE id=$1", module).Scan(&confirmed, &state))
				require.False(t, confirmed, "commands require a fresh post-assignment confirmation")
				require.Equal(t, "unknown", state)
			}
			var hash, power sql.NullFloat64
			var after []byte
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT hashrate_hs,power_w,source_times FROM container_module_latest_metrics WHERE module_id=$1", module).Scan(&hash, &power, &after))
			require.False(t, hash.Valid, "prior placement readings cannot become current at the new placement")
			require.False(t, power.Valid)
			require.JSONEq(t, string(before), string(after), "invalidation must preserve source watermarks")
			var history float64
			require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT hashrate_hs FROM container_module_metrics WHERE module_id=$1", module).Scan(&history))
			require.Equal(t, 100e12, history, "historical readings remain intact")
		})
	}
}

func TestPSUMoveFiltersModuleRollupWithoutParentMetrics(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	moved := r.snapshot(time.Second, false, 999e12, 200e12)
	m := &moved.Tanks[0].Modules[0]
	m.Position = 2
	m.Hashboards[0].ObservedAt = r.base
	m.Hashboards[0].StateObservedAt = r.base
	sdk.CalculateContainerRollups(moved)
	moved.Metrics = nil
	r.store(t, moved)
	var hash, temperature sql.NullFloat64
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT hashrate_hs,temperature_c FROM container_module_metrics WHERE collected_at=$1 AND module_id=(SELECT id FROM container_module WHERE reported_id='module-1')", moved.ObservedAt).Scan(&hash, &temperature))
	require.False(t, hash.Valid, "module rollups must use attributed boards even without parent metrics")
	require.False(t, temperature.Valid)
}
