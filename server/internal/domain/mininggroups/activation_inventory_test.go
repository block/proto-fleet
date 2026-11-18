package mininggroups

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActivationRequiresOperatorScope(t *testing.T) {
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1, AuthMethod: session.AuthMethodSession})
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, uuid.NewString())
	require.NoError(t, err)
	_, err = (&Service{}).Activate(ctx, "container", uuid.NewString())
	require.True(t, fleeterror.IsForbiddenError(err))
}

func TestActivationRetainsPoolIntentUntilPlacementIsComplete(t *testing.T) {
	ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t, true)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE module_id=(SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL) AND position=1`, boards[0])
	require.NoError(t, err)
	writes := miner.writes
	operationID, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err, "an omitted physical slot must not hide identified boards from activation")
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	require.True(t, miner.snapshot.Enabled)
	require.Equal(t, writes, miner.writes, "activation must keep the controller's existing pool stack")
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, source.RetiredAt.Valid, "the container intent protects boards whose placement is incomplete")
	var pending bool
	var inherited int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT placement_pending AND completed_at IS NOT NULL FROM mining_group_pool_handoff WHERE organization_id=$1 AND operation_id=$2`, f.target.OrganizationID, operationID).Scan(&pending))
	require.True(t, pending)
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='hashboard_installation' AND retired_at IS NULL`, f.target.DeviceID).Scan(&inherited))
	require.Zero(t, inherited, "the retained source must not coexist with a partial board handoff")

	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,1,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0])
	require.NoError(t, err)
	var recoveryCalls []sdk.MiningGroupRequest
	miner.beforeCall = func(_ context.Context, request sdk.MiningGroupRequest) {
		recoveryCalls = append(recoveryCalls, request)
	}
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err, "fresh placement must finish the handoff without a new activation request")
	source, err = sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, source.RetiredAt.Valid)
	assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Len(t, assignments.Assignments, len(boards))
	for _, assignment := range assignments.Assignments {
		require.Equal(t, config.ID, assignment.ConfigurationID)
	}
	require.Equal(t, writes, miner.writes, "placement recovery must only change database intent")
	for _, call := range recoveryCalls {
		require.True(t, call.ReadOnly(), "placement recovery must not change controller settings")
	}
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT placement_pending FROM mining_group_pool_handoff WHERE organization_id=$1 AND operation_id=$2`, f.target.OrganizationID, operationID).Scan(&pending))
	require.False(t, pending)
}

func TestActivationDoesNotRequireAnObservationForEveryEmptySlot(t *testing.T) {
	ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE module_id=(SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL) AND position=2`, boards[0])
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Len(t, assignments.Assignments, 1)
	require.Equal(t, config.ID, assignments.Assignments[0].ConfigurationID)
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, source.RetiredAt.Valid)
	require.Equal(t, 1, miner.writes)
}

func TestActivationPreservesPoolIntentForABoardWithoutAnInstallation(t *testing.T) {
	ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t)
	var unlocated int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&unlocated))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES($1,'mac','02:00:00:00:00:02',$2),($1,'serial','board-unlocated',$2)`, f.target.OrganizationID, unlocated)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_observation(hashboard_id,device_id,site_id,last_seen_at) SELECT $1,id,site_id,now() FROM device WHERE id=$2`, unlocated, f.target.DeviceID)
	require.NoError(t, err)
	miner.inventory.Boards = append(miner.inventory.Boards, sdk.HashboardIdentityClaim{MACAddress: "02:00:00:00:00:02", Serial: "board-unlocated"})
	miner.snapshot.Groups[0].MACs = append(miner.snapshot.Groups[0].MACs, "02:00:00:00:00:02")
	operation, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	var captured string
	var pending bool
	var installations int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT captured::text,placement_pending FROM mining_group_pool_handoff WHERE organization_id=$1 AND operation_id=$2`, f.target.OrganizationID, operation).Scan(&captured, &pending))
	require.True(t, pending)
	require.Contains(t, captured, "board-unlocated", "the frozen handoff must retain an inventory-only board")
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hashboard_installation WHERE hashboard_id=$1`, unlocated).Scan(&installations))
	require.Zero(t, installations, "activation must not manufacture a physical placement")
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, source.RetiredAt.Valid)

	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,installed_at,last_seen_at,metadata) SELECT $1,module_id,2,site_id,now(),now(),'{"macAddress":"02:00:00:00:00:02"}' FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, unlocated, boards[0])
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET hashboard_id=$1,occupancy='occupied',observed_at=now() WHERE module_id=(SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL) AND position=2`, unlocated)
	require.NoError(t, err)
	var recoveryCalls []sdk.MiningGroupRequest
	miner.beforeCall = func(_ context.Context, request sdk.MiningGroupRequest) {
		recoveryCalls = append(recoveryCalls, request)
	}
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: append(boards, unlocated)})
	require.NoError(t, err)
	require.Len(t, assignments.Assignments, 2)
	for _, assignment := range assignments.Assignments {
		require.Equal(t, config.ID, assignment.ConfigurationID)
	}
	require.Equal(t, 1, miner.writes)
	for _, call := range recoveryCalls {
		require.True(t, call.ReadOnly(), "placement recovery must not change controller settings")
	}
}

func TestActivationRejectsUnverifiedInventoryBeforeChangingTheController(t *testing.T) {
	for _, scenario := range []string{"malformed MAC", "missing MAC", "missing serial", "duplicate MAC", "duplicate serial", "missing identity", "stale inventory", "other account", "other controller"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
			miner.inventory = &sdk.MiningGroupInventory{ObservedAt: time.Now().UTC(), Boards: []sdk.HashboardIdentityClaim{{MACAddress: "02:00:00:00:00:01", Serial: "board-1"}}}
			var err error
			switch scenario {
			case "malformed MAC":
				miner.inventory.Boards[0].MACAddress = "not-a-mac"
			case "missing MAC":
				miner.inventory.Boards[0].MACAddress = ""
			case "missing serial":
				miner.inventory.Boards[0].Serial = ""
			case "duplicate MAC":
				miner.inventory.Boards = append(miner.inventory.Boards, sdk.HashboardIdentityClaim{MACAddress: "02:00:00:00:00:01", Serial: "another-board"})
			case "duplicate serial":
				miner.inventory.Boards = append(miner.inventory.Boards, sdk.HashboardIdentityClaim{MACAddress: "02:00:00:00:00:02", Serial: "board-1"})
			case "missing identity":
				_, err = f.conn.ExecContext(ctx, `DELETE FROM hashboard_identity WHERE hashboard_id=$1`, boards[0])
			case "stale inventory":
				miner.inventory.ObservedAt = time.Now().Add(-5 * time.Minute)
			case "other account":
				var account int64
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Other client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, account)
			case "other controller":
				q := sqlc.New(f.conn)
				identifier := uuid.NewString()
				discovery, createErr := q.UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{OrgID: f.target.OrganizationID, DeviceIdentifier: identifier, IpAddress: "127.0.254.1", Port: "8080", UrlScheme: "http", IsActive: true, DriverName: "proto"})
				require.NoError(t, createErr)
				controller, createErr := q.InsertDevice(ctx, sqlc.InsertDeviceParams{OrgID: f.target.OrganizationID, DiscoveredDeviceID: discovery, DeviceIdentifier: identifier, MacAddress: "02:00:00:00:ff:01"})
				require.NoError(t, createErr)
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_observation SET device_id=$1 WHERE hashboard_id=$2`, controller, boards[0])
			}
			require.NoError(t, err)
			writes := miner.writes
			sourceBefore, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
			require.NoError(t, err)
			var calls []sdk.MiningGroupRequest
			miner.beforeCall = func(_ context.Context, request sdk.MiningGroupRequest) { calls = append(calls, request) }
			_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			if scenario == "other account" {
				require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
			} else {
				require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
				require.ErrorContains(t, err, "inventory")
			}
			require.Equal(t, "idle", f.state(t).Status)
			require.False(t, miner.snapshot.Enabled)
			require.Equal(t, writes, miner.writes)
			for _, call := range calls {
				require.True(t, call.ReadOnly(), "invalid identity evidence must fail before a controller mutation")
			}
			source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
			require.NoError(t, err)
			require.Equal(t, sourceBefore, source, "rejected activation must preserve the current assignment state")
		})
	}
}

func TestActivationRetainedPoolIntentRejectsRemovalAndEdit(t *testing.T) {
	ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t)
	removal, err := service.ReviewRemoval(ctx, "", poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: f.target.DeviceID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE hashboard_id=$1`, boards[0])
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	_, err = service.ConfirmReview(ctx, "", removal.ID, poolconfigurations.Confirmation{KeepDeviceSettings: true})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	require.ErrorContains(t, err, "locations")
	password := "changed-password"
	_, err = service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: "Changed", Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: "stratum+tcp://changed.example:3333", Username: "account", WorkerTemplate: "{miner_name}", Password: &password}}})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	require.ErrorContains(t, err, "locations")
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, source.RetiredAt.Valid)
	require.Equal(t, config.ID, source.ConfigurationID)
	require.Equal(t, config.RevisionID, source.RevisionID)
	require.Equal(t, 1, miner.writes)
}

func TestActivationRetainedPoolIntentBlocksOwnershipChanges(t *testing.T) {
	ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE hashboard_id=$1`, boards[0])
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	var account int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'New client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, account)
	require.ErrorContains(t, err, "activation pool assignment transfer", "ownership must not retire the source while its board handoff is pending")
	var allocated int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_asset WHERE organization_id=$1 AND kind=7 AND asset_id=$2 AND client_id IS NOT NULL`, f.target.OrganizationID, boards[0]).Scan(&allocated))
	require.Zero(t, allocated)
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, source.RetiredAt.Valid)
	require.Equal(t, 1, miner.writes)
}

func TestActivationPlacementRecoveryAfterInterruptedResume(t *testing.T) {
	ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE hashboard_id=$1`, boards[0])
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	// An interrupted resume can leave enabled groups and a retained assignment.
	// Location recovery can finish the database transfer without restarting mining.
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='failed' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0])
	require.NoError(t, err)
	writes, calls := miner.writes, len(miner.calls)
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.Equal(t, "failed", f.state(t).Status)
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, source.RetiredAt.Valid)
	require.Equal(t, writes, miner.writes)
	for _, call := range miner.calls[calls:] {
		require.True(t, call.ReadOnly())
	}
}
