package timescaledb

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func replacementAccount(t *testing.T, r *modulePSUIdentityRig, parent int64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
	 SELECT gen_random_uuid()::text,organization_id,id,depth+1,'Replacement owner',31 FROM hosted_account
	 WHERE organization_id=$1 AND ((id=$2 AND $2<>0) OR (depth=0 AND $2=0)) RETURNING id`, r.orgID, parent).Scan(&id))
	return id
}

func TestAutomaticHashboardReplacementKeepsOtherOwnershipDecisions(t *testing.T) {
	for _, scenario := range []string{"different PSU", "old board moved", "old board moved then removed", "replacement already owned", "source recalled", "owner changed after removal", "replacement manually recalled", "site denied", "suspended", "same board new MAC", "conflicting identity"} {
		t.Run(scenario, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			initial := replacementSnapshot(r.base, "old", "other", "spare")
			initial.Tanks[0].Modules = append(initial.Tanks[0].Modules, confirmedModule(r.base, 2, "psu-b", identifiedBoard(r.base, "stock")))
			require.NoError(t, r.store(t, initial))
			old, stock := r.boardID(t, "old"), r.boardID(t, "stock")
			client := replacementAccount(t, r, 0)
			child := replacementAccount(t, r, client)
			otherClient := replacementAccount(t, r, 0)
			assignReplacementBoard(t, r, old, client, child, r.base.Add(time.Millisecond))
			at := r.base.Add(time.Second)
			next := replacementSnapshot(at, "new", "other", "spare")
			wantOld := [2]int64{client, child}
			switch scenario {
			case "different PSU":
				next.Tanks[0].Modules[0].ReportedID = "replacement-psu"
			case "old board moved":
				next = replacementSnapshot(at, "new", "other", "old")
			case "old board moved then removed":
				require.NoError(t, r.store(t, replacementSnapshot(at, "", "other", "old")))
				next = replacementSnapshot(at.Add(time.Second), "new", "other", "")
			case "replacement already owned":
				assignReplacementBoard(t, r, stock, otherClient, 0, r.base.Add(time.Millisecond))
				next = replacementSnapshot(at, "stock", "other", "spare")
			case "source recalled":
				assignReplacementBoard(t, r, old, 0, 0, r.base.Add(2*time.Millisecond))
				wantOld = [2]int64{}
			case "owner changed after removal":
				require.NoError(t, r.store(t, replacementSnapshot(at, "", "other", "spare")))
				assignReplacementBoard(t, r, old, otherClient, 0, at.Add(time.Millisecond))
				next = replacementSnapshot(at.Add(time.Second), "new", "other", "spare")
				wantOld = [2]int64{otherClient, 0}
			case "replacement manually recalled":
				next.Tanks[0].Modules[0].IdentityConfirmed = false
				require.NoError(t, r.store(t, next))
				incoming := r.boardID(t, "new")
				assignReplacementBoard(t, r, incoming, otherClient, 0, at.Add(time.Millisecond))
				assignReplacementBoard(t, r, incoming, 0, 0, at.Add(2*time.Millisecond))
				next = replacementSnapshot(at.Add(time.Second), "new", "other", "spare")
			case "site denied":
				_, err := r.db.ExecContext(t.Context(), `UPDATE hosted_account SET sites_restricted=true WHERE id=$1`, child)
				require.NoError(t, err)
			case "suspended":
				_, err := r.db.ExecContext(t.Context(), `UPDATE hosted_account SET suspended=true WHERE id=$1`, client)
				require.NoError(t, err)
			case "same board new MAC":
				next = replacementSnapshot(at, "old", "other", "spare")
				next.Tanks[0].Modules[0].Hashboards[0].MACAddress = "changed-mac"
			case "conflicting identity":
				next.Tanks[0].Modules[0].Hashboards[0].MACAddress = "mac-old"
			}
			require.NoError(t, r.store(t, next))
			require.Equal(t, wantOld, replacementOwner(t, r, old))
			if scenario == "replacement already owned" {
				require.Equal(t, [2]int64{otherClient, 0}, replacementOwner(t, r, stock))
			}
			require.Zero(t, replacementEventCount(t, r))
		})
	}
}

func TestAutomaticHashboardReplacementRollsBackWithInventory(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
	old := r.boardID(t, "old")
	client := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	// Another allocation operation in this transaction must retain its own boundary.
	previous := r.base.Add(time.Minute).Format(time.RFC3339Nano)
	_, err = tx.ExecContext(t.Context(), `SELECT set_config('fleet.allocation_boundary',$1,true)`, previous)
	require.NoError(t, err)
	require.NoError(t, storeContainerInventory(t.Context(), sqlc.New(tx), r.device, replacementSnapshot(r.base.Add(time.Second), "new", "other", "spare")))
	var restored string
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT current_setting('fleet.allocation_boundary')`).Scan(&restored))
	require.Equal(t, previous, restored)
	var clientID sql.NullInt64
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT client_id FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, old).Scan(&clientID))
	require.False(t, clientID.Valid, "transfer occurs inside the inventory transaction")
	require.NoError(t, tx.Rollback())
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Zero(t, replacementEventCount(t, r))
}

func TestAutomaticHashboardReplacementUsesMACsAndPreservesEachSlot(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	initial := replacementSnapshot(r.base, "old-a", "old-b", "old-c")
	initial.Tanks[0].Modules = append(initial.Tanks[0].Modules, confirmedModule(r.base, 2, "psu-b", identifiedBoard(r.base, "stock")))
	for i := range initial.Tanks[0].Modules[0].Hashboards {
		initial.Tanks[0].Modules[0].Hashboards[i].Serial = ""
	}
	require.NoError(t, r.store(t, initial))
	module := r.moduleIDAt(t, 1, 1)
	boardByMAC := func(mac string) int64 {
		var id int64
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_identity WHERE org_id=$1 AND kind='mac' AND value=$2`, r.orgID, mac).Scan(&id))
		return id
	}
	old := []int64{boardByMAC("mac-old-a"), boardByMAC("mac-old-b"), boardByMAC("mac-old-c")}
	client := replacementAccount(t, r, 0)
	child := replacementAccount(t, r, client)
	otherClient := replacementAccount(t, r, 0)
	owners := [][2]int64{{client, 0}, {client, child}, {otherClient, 0}}
	for i, board := range old {
		assignReplacementBoard(t, r, board, owners[i][0], owners[i][1], r.base.Add(time.Millisecond))
	}
	// A spare recalled before installation can inherit its new slot's owner.
	stock := r.boardID(t, "stock")
	assignReplacementBoard(t, r, stock, otherClient, 0, r.base.Add(time.Millisecond))
	assignReplacementBoard(t, r, stock, 0, 0, r.base.Add(2*time.Millisecond))
	at := r.base.Add(time.Second)
	next := replacementSnapshot(at, "stock", "new-b", "new-c")
	next.Tanks[0].Modules[0].Position = 2
	for i := 1; i < 3; i++ {
		next.Tanks[0].Modules[0].Hashboards[i].Serial = ""
	}
	require.NoError(t, r.store(t, next))
	require.Equal(t, module, r.moduleIDAt(t, 1, 2), "slot ownership follows the PSU when its position changes")
	incoming := []int64{stock, boardByMAC("mac-new-b"), boardByMAC("mac-new-c")}
	for i, board := range incoming {
		require.Equal(t, [2]int64{}, replacementOwner(t, r, old[i]))
		require.Equal(t, owners[i], replacementOwner(t, r, board))
	}
	require.Equal(t, 3, replacementEventCount(t, r))
}

func TestAutomaticHashboardReplacementMigrationPreservesOwnership(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
	old := r.boardID(t, "old")
	client := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "new")))
	incoming := r.boardID(t, "new")
	for _, migration := range []string{
		"001049_internal_hashboard_allocation_order.down",
		"001048_internal_automatic_hashboard_allocation.down",
		"001048_internal_automatic_hashboard_allocation.up",
		"001049_internal_hashboard_allocation_order.up",
	} {
		script, err := migrations.Current.ReadFile("current/" + migration + ".sql")
		require.NoError(t, err)
		_, err = r.db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
		require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
		require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, incoming))
		var periods int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset_interval WHERE organization_id=$1 AND kind=7`, r.orgID).Scan(&periods))
		require.Equal(t, 2, periods)
		require.Equal(t, 1, replacementEventCount(t, r))
	}
	// Boards already present at upgrade can still be replaced by new installations.
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "newer")))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, r.boardID(t, "newer")))
	require.Equal(t, 2, replacementEventCount(t, r))
}

func assignReplacementBoard(t *testing.T, r *modulePSUIdentityRig, board, client, child int64, at time.Time) {
	t.Helper()
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT set_config('fleet.allocation_boundary',$1,true)`, at.Format(time.RFC3339Nano))
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id,subclient_id,updated_at)
	 VALUES($1,7,$2,$2,$3,NULLIF($4,0),NULLIF($5,0),$6)
	 ON CONFLICT(organization_id,kind,asset_id) DO UPDATE SET client_id=EXCLUDED.client_id,subclient_id=EXCLUDED.subclient_id,updated_at=EXCLUDED.updated_at,revision=hosted_account_asset.revision+1`, r.orgID, board, r.deviceID, client, child, at)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func replacementOwner(t *testing.T, r *modulePSUIdentityRig, board int64) [2]int64 {
	t.Helper()
	var owner [2]int64
	err := r.db.QueryRowContext(t.Context(), `SELECT COALESCE(client_id,0),COALESCE(subclient_id,0) FROM hosted_account_asset WHERE organization_id=$1 AND kind=7 AND asset_id=$2`, r.orgID, board).Scan(&owner[0], &owner[1])
	if err != sql.ErrNoRows {
		require.NoError(t, err)
	}
	return owner
}

func replacementEventCount(t *testing.T, r *modulePSUIdentityRig) int {
	t.Helper()
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='account_hashboard_replaced'`, r.orgID).Scan(&count))
	return count
}

func replacementSnapshot(at time.Time, serials ...string) *sdk.ContainerSnapshot {
	boards := make([]sdk.ContainerHashboard, sdk.CT1HashboardsPerModule)
	for i := range int32(sdk.CT1HashboardsPerModule) {
		serial := ""
		if int(i) < len(serials) {
			serial = serials[i]
		}
		if serial == "" {
			boards[i] = sdk.ContainerHashboard{Position: i + 1, Occupancy: "empty"}
			continue
		}
		boards[i] = identifiedBoard(at, serial)
		boards[i].Position = i + 1
		boards[i].MACAddress = "mac-" + serial
	}
	return modulePSUSnapshot(at, sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, 1, "psu-a", boards...)}})
}

func TestAutomaticHashboardReplacementPreservesSlotOwner(t *testing.T) {
	for _, gap := range []string{"direct", "empty slot", "unknown slot", "unconfirmed PSU", "missing PSU"} {
		t.Run(gap, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
			module := r.moduleIDAt(t, 1, 1)
			old, other := r.boardID(t, "old"), r.boardID(t, "other")
			client := replacementAccount(t, r, 0)
			child := replacementAccount(t, r, client)
			otherClient := replacementAccount(t, r, 0)
			assignedAt := r.base.Add(time.Millisecond)
			assignReplacementBoard(t, r, old, client, child, assignedAt)
			assignReplacementBoard(t, r, other, otherClient, 0, assignedAt)
			at := r.base.Add(time.Second)
			switch gap {
			case "empty slot", "unknown slot":
				snapshot := replacementSnapshot(at, "", "other", "spare")
				if gap == "unknown slot" {
					snapshot.Tanks[0].Modules[0].Hashboards[0].Occupancy = "unknown"
				}
				require.NoError(t, r.store(t, snapshot))
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, old))
				at = at.Add(time.Second)
			case "unconfirmed PSU", "missing PSU":
				snapshot := replacementSnapshot(at, "new", "other", "spare")
				snapshot.Tanks[0].Modules[0].IdentityConfirmed = false
				if gap == "missing PSU" {
					snapshot.Tanks[0].Modules[0].ReportedID = ""
				}
				require.NoError(t, r.store(t, snapshot))
				require.Equal(t, [2]int64{}, replacementOwner(t, r, r.boardID(t, "new")))
				at = at.Add(time.Second)
			}
			require.NoError(t, r.store(t, replacementSnapshot(at, "new", "other", "spare")))
			incoming := r.boardID(t, "new")
			require.Equal(t, module, r.moduleIDAt(t, 1, 1))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, incoming))
			require.Equal(t, [2]int64{otherClient, 0}, replacementOwner(t, r, other))
			var ended, started time.Time
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND started_at=$2`, old, assignedAt).Scan(&ended))
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, incoming).Scan(&started))
			require.Equal(t, at, ended.UTC())
			require.Equal(t, at, started.UTC())
			// Replays and later unchanged reports must not transfer or audit twice.
			require.NoError(t, r.store(t, replacementSnapshot(at, "new", "other", "spare")))
			require.NoError(t, r.store(t, replacementSnapshot(at.Add(time.Second), "new", "other", "spare")))
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
			var events int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='account_hashboard_replaced' AND actor_type='system'
			 AND metadata->>'automatic'='true' AND (metadata->>'old_hashboard_id')::bigint=$2 AND (metadata->>'replacement_hashboard_id')::bigint=$3`, r.orgID, old, incoming).Scan(&events))
			require.Equal(t, 1, events)
		})
	}
}

func TestAutomaticHashboardReplacementRespectsManualDecisionsAfterClockAheadPoll(t *testing.T) {
	for _, decision := range []string{"incoming recall", "outgoing reallocation", "same-owner allocation"} {
		t.Run(decision, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
			old := r.boardID(t, "old")
			client := replacementAccount(t, r, 0)
			other := replacementAccount(t, r, 0)
			assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
			at := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
			pending := replacementSnapshot(at, "new")
			pending.Tanks[0].Modules[0].IdentityConfirmed = false
			require.NoError(t, r.store(t, pending))
			incoming := r.boardID(t, "new")
			manualAt := time.Now().UTC().Truncate(time.Microsecond)
			require.True(t, manualAt.Before(at), "the manual action follows ingestion but precedes its observation timestamp")
			wantOld := [2]int64{client, 0}
			switch decision {
			case "incoming recall":
				assignReplacementBoard(t, r, incoming, other, 0, manualAt)
				assignReplacementBoard(t, r, incoming, 0, 0, manualAt.Add(time.Microsecond))
			case "outgoing reallocation":
				assignReplacementBoard(t, r, old, other, 0, manualAt)
				wantOld = [2]int64{other, 0}
			case "same-owner allocation":
				assignReplacementBoard(t, r, old, client, 0, manualAt)
			}
			require.NoError(t, r.store(t, replacementSnapshot(at.Add(time.Millisecond), "new")))
			require.Equal(t, wantOld, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
			require.Zero(t, replacementEventCount(t, r))
		})
	}
}

func TestAutomaticHashboardReplacementAcceptsRecalledSpareFromAnotherController(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		t.Run(map[bool]string{false: "immediate", true: "delayed PSU confirmation"}[delayed], func(t *testing.T) {
			database := testutil.NewDatabaseService(t, nil)
			user := database.CreateSuperAdminUser()
			base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
			var rigs []*modulePSUIdentityRig
			for range 2 {
				device := database.CreateDevice(user.OrganizationID, "proto")
				_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
				require.NoError(t, err)
				rigs = append(rigs, &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, device: device.ID, orgID: user.OrganizationID, base: base})
			}
			destination, source := rigs[0], rigs[1]
			require.NoError(t, destination.store(t, replacementSnapshot(base, "old")))
			stockSnapshot := replacementSnapshot(base, "stock")
			stockSnapshot.Tanks[0].Modules[0].ReportedID = "stock-psu"
			require.NoError(t, source.store(t, stockSnapshot))
			old, stock := destination.boardID(t, "old"), source.boardID(t, "stock")
			client := replacementAccount(t, destination, 0)
			child := replacementAccount(t, destination, client)
			other := replacementAccount(t, source, 0)
			assignReplacementBoard(t, destination, old, client, child, base.Add(time.Millisecond))
			assignReplacementBoard(t, source, stock, other, 0, base.Add(time.Millisecond))
			assignReplacementBoard(t, source, stock, 0, 0, base.Add(2*time.Millisecond))
			require.NoError(t, source.store(t, modulePSUSnapshot(base.Add(time.Second))))
			at := base.Add(2 * time.Second)
			next := replacementSnapshot(at, "stock")
			if delayed {
				next.Tanks[0].Modules[0].IdentityConfirmed = false
				require.NoError(t, destination.store(t, next))
				require.Equal(t, [2]int64{}, replacementOwner(t, destination, stock))
				at = at.Add(time.Second)
			}
			require.NoError(t, destination.store(t, replacementSnapshot(at, "stock")))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, destination, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, destination, stock))
			// The other controller must confirm a complete map after installation.
			require.NoError(t, source.store(t, modulePSUSnapshot(at.Add(time.Second))))
			at = at.Add(2 * time.Second)
			require.NoError(t, destination.store(t, replacementSnapshot(at, "stock")))
			require.Equal(t, [2]int64{}, replacementOwner(t, destination, old))
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, destination, stock))
			var device int64
			require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT device_id FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, stock).Scan(&device))
			require.Equal(t, destination.deviceID, device)
			require.NoError(t, destination.store(t, replacementSnapshot(at.Add(time.Second), "stock")))
			require.Equal(t, 1, replacementEventCount(t, destination))
		})
	}
}

func TestAutomaticHashboardReplacementWaitsForHistoryBoundary(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "stock")))
	old, stock := r.boardID(t, "old"), r.boardID(t, "stock")
	client := replacementAccount(t, r, 0)
	other := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	assignReplacementBoard(t, r, stock, other, 0, r.base.Add(time.Millisecond))
	assignReplacementBoard(t, r, stock, 0, 0, r.base.Add(10*time.Millisecond))
	// The report arrives after the recall but carries an older observation time.
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(5*time.Millisecond), "stock", "")))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, stock))
	require.Zero(t, replacementEventCount(t, r))
	at := r.base.Add(20 * time.Millisecond)
	require.NoError(t, r.store(t, replacementSnapshot(at, "stock", "")))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, stock))
	var ended, started time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND client_id=$2`, stock, other).Scan(&ended))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, stock).Scan(&started))
	require.Equal(t, at, started.UTC())
	require.True(t, ended.Before(started))
	require.Equal(t, 1, replacementEventCount(t, r))
}

func TestAutomaticHashboardReplacementDoesNotInferPreUpgradeOrdering(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
	old := r.boardID(t, "old")
	client := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	pending := replacementSnapshot(r.base.Add(time.Second), "new")
	pending.Tanks[0].Modules[0].IdentityConfirmed = false
	require.NoError(t, r.store(t, pending))
	// Reapply the migration with an existing pending replacement. It cannot
	// reconstruct which manual decisions followed its installation or removal.
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Current.ReadFile("current/001049_internal_hashboard_allocation_order." + direction + ".sql")
		require.NoError(t, err)
		_, err = r.db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
	}
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "new")))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, r.boardID(t, "new")))
	require.Zero(t, replacementEventCount(t, r))
}
