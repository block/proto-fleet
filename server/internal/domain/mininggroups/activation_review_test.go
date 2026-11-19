package mininggroups

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRetainedActivationBlocksUncapturedBoardAllocation(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t, true)
	completePendingActivation(t, ctx, f, boards[0])
	// Recreate a retained container handoff whose capture excludes the new board.
	_, err := f.conn.ExecContext(ctx, `UPDATE mining_group_pool_handoff SET captured=(captured-'PartialTransfer') || jsonb_build_object('Inventory','[]'::jsonb) WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	q := sqlc.New(f.conn)
	ready, err := q.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, ready, "retained container intent must block allocation of uncaptured boards")
	reason, err := q.GetCT1HashboardAllocationBlocker(ctx, sqlc.GetCT1HashboardAllocationBlockerParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Contains(t, reason, "pool assignment transfer")
	var account int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[1], f.target.DeviceID, account)
	require.ErrorContains(t, err, "ownership cannot change")
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id) VALUES($1,7,$2,$2,$3)`, f.target.OrganizationID, boards[1], f.target.DeviceID)
	require.NoError(t, err, "new operator-owned inventory must still synchronize")
	_, err = f.conn.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=$2 WHERE kind=7 AND asset_id=$1`, boards[1], account)
	require.ErrorContains(t, err, "ownership cannot change")

}

func TestActivationWaitsForFreshInventoryAfterStop(t *testing.T) {
	for _, scenario := range []string{"controller timestamp", "board timestamp"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
			miner.running = true
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			for range 6 {
				if !f.state(t).ActivationStopRequested {
					f.reconcile(t, ctx)
				}
			}
			require.True(t, f.state(t).ActivationStopRequested)
			require.False(t, miner.running)
			if scenario == "controller timestamp" {
				miner.inventory.ObservedAt = time.Now().Add(-3 * time.Minute)
			} else {
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_observation SET last_seen_at=now()-interval '3 minutes' WHERE hashboard_id=$1`, boards[0])
				require.NoError(t, err)
			}
			f.reconcile(t, ctx)
			require.Equal(t, "blocked", f.state(t).Status)
			miner.inventory.ObservedAt = time.Now()
			_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_observation SET last_seen_at=now() WHERE hashboard_id=$1`, boards[0])
			require.NoError(t, err)
			for range 12 {
				if active(f.state(t).Status) {
					f.reconcile(t, ctx)
				}
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			require.False(t, miner.running, "activation must preserve main's explicit wake requirement")
		})
	}
}

type inventoryFailureMiner struct {
	*activationPoolMiner
	failure error
}

func (m inventoryFailureMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m inventoryFailureMiner) MiningGroups(context.Context, sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	return sdk.MiningGroupResult{}, m.failure
}
func TestActivationInventoryKeepsPrivateDiagnosticCause(t *testing.T) {
	ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
	cause := errors.New("private controller diagnostic")
	f.service.miners = inventoryFailureMiner{miner, cause}
	_, err := f.service.fetchActivationInventory(ctx, f.target.DeviceID)
	require.ErrorIs(t, err, cause)
	require.True(t, isInventoryStale(err))
	require.NotContains(t, err.Error(), cause.Error())
}

func TestPartialActivationAllowsUncapturedBoardAllocation(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t, true)
	completePendingActivation(t, ctx, f, boards[0])
	// A later board is outside the saved inventory; only unresolved identity intent stays fenced.
	_, err := f.conn.ExecContext(ctx, `UPDATE mining_group_pool_handoff SET captured=jsonb_set(captured,'{Inventory}',(SELECT jsonb_agg(b) FROM jsonb_array_elements(captured->'Inventory') b WHERE (b->>'hashboard_id')::bigint<>$2)) WHERE device_id=$1`, f.target.DeviceID, boards[1])
	require.NoError(t, err)
	q := sqlc.New(f.conn)
	ready, err := q.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, ready)
	var account, installation int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[1], f.target.DeviceID, account)
	require.NoError(t, err)
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[1]).Scan(&installation))
	pending, err := q.HasPendingActivationPoolTarget(ctx, sqlc.HasPendingActivationPoolTargetParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID, TargetKind: "hashboard_installation", TargetID: installation})
	require.NoError(t, err)
	require.False(t, pending, "new customer board must not inherit another board's pool fence")
	var activeAssignments int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1 AND retired_at IS NULL AND (target_kind='device' OR target_id=$2)`, f.target.DeviceID, installation).Scan(&activeAssignments))
	require.Zero(t, activeAssignments, "allocation retires operator intent for the newly owned board")
}

type inventoryUpgradeMiner struct{ *activationPoolMiner }

func (m inventoryUpgradeMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m inventoryUpgradeMiner) CheckMiningGroupActivationInventory(context.Context) error {
	return fleeterror.NewFailedPreconditionError("Upgrade the Fleet Node and reconnect it before enabling mining groups.")
}
func TestActivationChecksNodeVersionBeforeInventoryAdmission(t *testing.T) {
	ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
	f.service.miners = inventoryUpgradeMiner{miner}
	miner.beforeCall = func(_ context.Context, request sdk.MiningGroupRequest) {
		require.NotEqual(t, "activation_inventory", request.Action)
	}
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "Upgrade the Fleet Node")
	var journals int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_pool_handoff WHERE device_id=$1`, f.target.DeviceID).Scan(&journals))
	require.Zero(t, journals)
}
