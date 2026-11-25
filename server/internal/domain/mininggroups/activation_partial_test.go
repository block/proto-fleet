package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestPartialActivationEditRestartAndRepair(t *testing.T) {
	ctx, f, miner, service, original, boards, confirm := newActivationPoolFixture(t, true)
	completePendingActivation(t, ctx, f, boards[0])
	q := sqlc.New(f.conn)
	pending, err := q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, boards[0], pending[0].HashboardID)
	shown, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Len(t, shown.Assignments, 2)
	for _, a := range shown.Assignments {
		if a.HashboardID == boards[0] {
			require.Equal(t, "placement_pending", a.State)
		} else {
			require.Equal(t, original.ID, a.ConfigurationID)
		}
	}
	_, err = service.ReviewBoards(ctx, "", original.ID, original.Version, poolconfigurations.BoardSelection{HashboardIDs: boards[:1]})
	require.Error(t, err, "pending board cannot receive a new assignment")
	password := "pool-b-password"
	changed, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "B", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://updated.example:3333", Username: "b", Password: &password}}})
	require.NoError(t, err)
	review, err := service.ReviewBoards(ctx, "", changed.ID, changed.Version, poolconfigurations.BoardSelection{HashboardIDs: boards[1:]})
	require.NoError(t, err, "a mapped sibling in an incomplete module remains editable")
	saved := confirm(review)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
	for range 20 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	op, err := service.GetOperation(ctx, "", saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "verified", op.Targets[0].State, "journal=%s target=%+v", f.state(t).Message, op.Targets[0])
	// The unresolved board stays in default with its original pool settings.
	require.Contains(t, miner.snapshot.Groups[0].MACs, pending[0].MacAddress)
	require.Equal(t, "stratum+tcp://pool.example:3333", miner.snapshot.Groups[0].Pools[0].URL)
	f.service = NewService(f.conn, miner, f.service.pools, f.service.secrets)
	f.service.WithPoolConfigurations(poolconfigurations.NewRepository(f.service.secrets), poolapplications.NewReviewer(f.service.pools, sharedCapabilities{}))
	require.NoError(t, f.service.finishPendingActivationPlacement(ctx, f.target.OrganizationID, f.target.DeviceID))
	pending, err = q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0])
	require.NoError(t, err)
	calls := len(miner.calls)
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	shown, err = service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	for _, a := range shown.Assignments {
		if a.HashboardID == boards[0] {
			require.Equal(t, original.ID, a.ConfigurationID)
		} else {
			require.Equal(t, changed.ID, a.ConfigurationID)
		}
	}
	pending, err = q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, pending)
	for _, call := range miner.calls[calls:] {
		require.True(t, call.ReadOnly(), "recovery must not reapply either pool stack")
	}
}

func TestPartialActivationRecoveryRejectsChangedOwner(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t)
	completePendingActivation(t, ctx, f, boards[0])
	var account int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'New client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, account)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0])
	require.NoError(t, err)
	require.NoError(t, f.service.finishPendingActivationPlacement(ctx, f.target.OrganizationID, f.target.DeviceID))
	pending, err := sqlc.New(f.conn).ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	var assigned int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='hashboard_installation' AND retired_at IS NULL`, f.target.DeviceID).Scan(&assigned))
	require.Zero(t, assigned)
}

func TestPendingBoardIntentPreventsSchemaRollback(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t)
	completePendingActivation(t, ctx, f, boards[0])
	down, err := migrations.Current.ReadFile("current/001099_internal_activation_board_intent.down.sql")
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "activation board intent is pending")
	pending, err := sqlc.New(f.conn).ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, pending, 1)
}
