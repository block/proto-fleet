package mininggroups

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type undispatchedContainerClear struct{ *admittedGroups }

type legacyAndBoardClearCapabilities struct{ removalCapabilities }

func (legacyAndBoardClearCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true, sdk.CapabilityPoolClear: true, sdk.CapabilityMiningGroupPoolClear: true}
}

func (m *undispatchedContainerClear) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m *undispatchedContainerClear) VerifyPoolsCleared(context.Context) (bool, error) {
	return false, errors.New("pre-dispatch read failed")
}

func TestSiblingClearsSurviveLegacyPreparationFailureAndExternalModeChange(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	for position := 2; position <= 3; position++ {
		var board int64
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&board))
		_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
		 SELECT $1,module_id,$3,now(),now(),jsonb_build_object('macAddress',$4::text) FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, board, boards[0], position, fmt.Sprintf("02:00:00:00:00:0%d", position))
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1`, board)
		require.NoError(t, err)
		boards = append(boards, board)
	}
	publishSharedBoardInventory(t, ctx, f)
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: false, Revision: 1, MaxGroups: 4}, receipts: map[string]sdk.MiningGroupResult{}}
	getter := &undispatchedContainerClear{&admittedGroups{firmware}}
	f.service.miners = getter
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, legacyAndBoardClearCapabilities{})
	checker := poolapplications.NewChecker(f.conn, reviewer, getter, f.service.secrets, f.service.pools)
	f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, getter, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
	ids := []uuid.UUID{}
	for _, board := range boards {
		var id uuid.UUID
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, board).Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, worker.ProcessLifecycle(ctx, ids[0]))
	first, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, ids[len(ids)-1])
	require.NoError(t, err)
	require.Equal(t, "ready", first.State, "the failed read must leave a retryable whole-container clear")
	require.Equal(t, "device", first.TargetKind)
	for _, id := range ids[:len(ids)-1] {
		row, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "pending", row.State, "preparation must not discard a sibling clear")
	}
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.Error(t, err, "unfinished cleanup must block a mode change")
	// Firmware can change outside Fleet. Every original board must still have
	// a request when the failed legacy clear is reprepared in enabled mode.
	firmware.snapshot.Enabled = true
	firmware.snapshot.Groups = []sdk.MiningGroup{{ID: "default", Revision: 1, Mining: true, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"}, Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://previous.example", Username: "previous", Enabled: true}}}}
	for i := len(ids) - 1; i >= 0; i-- {
		id := ids[i]
		require.NoError(t, worker.ProcessLifecycle(ctx, id))
		for i := 0; i < 50 && active(f.state(t).Status); i++ {
			f.reconcile(t, ctx)
		}
		row, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "completed", row.State)
	}
	cleared := 0
	for _, group := range firmware.snapshot.Groups {
		if len(group.MACs) == 0 {
			continue
		}
		require.Empty(t, group.Pools)
		require.False(t, group.Mining)
		cleared += len(group.MACs)
	}
	require.Equal(t, 3, cleared)
}

func TestAutomaticClearRepreparesWholeContainerAsBoardAfterModeChange(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
	guarded := &admittedGroups{firmware}
	f.service.miners = guarded
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
	checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
	f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
	var id uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, boards[0]).Scan(&id))
	// A previous preparation selected the whole container with groups disabled,
	// but no attempt was admitted. Groups are now enabled; the original asset is
	// still a single board, so retry must discard the whole-container target.
	_, err = sqlc.New(f.conn).PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID, ReviewedBinding: "{}"})
	require.NoError(t, err)
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
	require.NoError(t, worker.ProcessLifecycle(ctx, id))
	for i := 0; i < 40 && active(f.state(t).Status); i++ {
		require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	}
	row, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.Equal(t, poolconfigurations.TargetInstallation, row.TargetKind)
	require.Equal(t, "completed", row.State)
	require.Equal(t, "complete", f.state(t).Status)
}
