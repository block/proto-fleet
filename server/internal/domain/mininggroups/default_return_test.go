package mininggroups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type defaultReturnMiner struct {
	*simulatedGroups
	loseReply      bool
	rejectRevision bool
	observeErr     bool
}

func (m *defaultReturnMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m *defaultReturnMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action == "observe" && m.observeErr {
		return sdk.MiningGroupResult{}, errors.New("controller unavailable")
	}
	if request.Action == operationDeactivate {
		m.snapshot.Enabled = false
		return sdk.MiningGroupResult{Deactivated: true}, nil
	}
	if request.Action == "transfer" && m.rejectRevision {
		m.rejectRevision = false
		return sdk.MiningGroupResult{RevisionConflict: true}, nil
	}
	result, err := m.simulatedGroups.MiningGroups(ctx, request)
	if request.Action == "transfer" && m.loseReply {
		m.loseReply = false
		return sdk.MiningGroupResult{}, errors.New("lost transfer response")
	}
	return result, err
}

func newDefaultReturnFixture(t *testing.T) (context.Context, retryFixture, *defaultReturnMiner, []int64) {
	t.Helper()
	ctx, f := newIdleRetryFixture(t)
	_, ids := preparePermissionPoolAssignment(t, ctx, f)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &defaultReturnMiner{simulatedGroups: &simulatedGroups{
		snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{
			{ID: "default", Revision: 1, Mining: true, Pools: []sdk.MiningGroupPool{{ID: 1, URL: "stratum+tcp://host.example:3333", Username: "host", Enabled: true}}},
			{ID: "cleared", Revision: 2, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:99"}},
		}}, receipts: map[string]sdk.MiningGroupResult{},
	}}
	f.service.miners = firmware
	_, err := f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	return ctx, f, firmware, ids
}

func reviewDefaultReturn(t *testing.T, ctx context.Context, f retryFixture, ids []int64) DefaultReturnReview {
	t.Helper()
	reviews, err := f.service.ReviewReturnToDefault(ctx, f.target.DeviceIdentifier, ids)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	require.Empty(t, reviews[0].BlockedReason)
	require.Len(t, reviews[0].Token, 64)
	return reviews[0]
}

func TestDefaultReturnIsOfferedOnlyAfterRecallCleanup(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.True(t, state.CanReturnToDefault)
	var client int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&client))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, ids[0], f.target.DeviceID, client)
	require.NoError(t, err)
	state, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.Contains(t, state.DeactivationBlockedReason, "Recall all allocated hashboards")
	require.False(t, state.CanReturnToDefault)
	_, err = f.conn.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	state, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.Contains(t, state.DeactivationBlockedReason, "ownership cleanup")
	require.False(t, state.CanReturnToDefault)
	_, err = f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	state, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.True(t, state.CanReturnToDefault)
	firmware.snapshot.Groups[1].MACs = nil
	state, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.Empty(t, state.DeactivationBlockedReason)
	require.False(t, state.CanReturnToDefault, "settled default membership permits Disable instead of Move")
}

func TestDefaultReturnPersistsExactTransferAfterLostReplyAndKeepsSibling(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	review := reviewDefaultReturn(t, ctx, f, ids)
	requestID := uuid.NewString()
	_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, requestID, ids)
	require.NoError(t, err)
	firmware.loseReply = true
	f.reconcile(t, ctx)
	prepared := f.state(t)
	require.True(t, prepared.Step.Valid)
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	f.service = NewService(f.conn, firmware, f.service.pools, f.service.secrets)
	f.reconcile(t, ctx)
	require.NotEmpty(t, f.state(t).ControllerOperationID)
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	require.Equal(t, statusComplete, f.state(t).Status)
	require.Equal(t, []string{"02:00:00:00:00:01"}, firmware.snapshot.Groups[0].MACs)
	require.Equal(t, []string{"02:00:00:00:00:99"}, firmware.snapshot.Groups[1].MACs)
	require.True(t, firmware.snapshot.Groups[0].Mining)
	require.Equal(t, "host", firmware.snapshot.Groups[0].Pools[0].Username)
	for _, call := range firmware.calls {
		if call.Action == "transfer" {
			require.Equal(t, prepared.StepKey.UUID.String(), call.Key)
			require.Equal(t, []string{"02:00:00:00:00:01"}, call.MACs)
		} else {
			require.Contains(t, []string{"observe", "operation"}, call.Action, "return must not change Default's pools or mining intent")
		}
	}
	// A lost submission reply also returns its original accepted request.
	result, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, requestID, ids)
	require.NoError(t, err)
	require.Equal(t, requestID, result.String())
	events := groupActivities(t, f)
	require.Len(t, events, 2)
	require.Equal(t, "mining_groups.return_default.completed", events[1].Type)
}

func TestDefaultReturnRequiresFreshReviewAndSettledCleanup(t *testing.T) {
	for _, change := range []string{"pools", "mining", "ownership", "installation", "cleanup", "permission"} {
		t.Run(change, func(t *testing.T) {
			ctx, f, firmware, ids := newDefaultReturnFixture(t)
			review := reviewDefaultReturn(t, ctx, f, ids)
			switch change {
			case "pools":
				firmware.snapshot.Groups[0].Pools[0].Username = "changed"
			case "mining":
				firmware.snapshot.Groups[0].Mining = false
			case "ownership":
				var client int64
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&client))
				_, err := f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, ids[0], f.target.DeviceID, client)
				require.NoError(t, err)
			case "installation":
				_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{"macAddress":"02:00:00:00:00:02"}' WHERE hashboard_id=$1`, ids[0])
				require.NoError(t, err)
			case "cleanup":
				_, err := f.conn.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership')`, f.target.OrganizationID, f.target.DeviceID, ids[0])
				require.NoError(t, err)
			case "permission":
				setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools, authz.PermMinerStopMining})
			}
			_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			_, err = f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
			require.Error(t, err)
			require.Equal(t, "idle", f.state(t).Status)
			require.Empty(t, firmware.receipts)
		})
	}
}

func TestDefaultReturnStopsWhenReviewedSettingsChangeAndCanBeRetried(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(map[bool]string{false: "before preparation", true: "revision conflict"}[prepared], func(t *testing.T) {
			ctx, f, firmware, ids := newDefaultReturnFixture(t)
			review := reviewDefaultReturn(t, ctx, f, ids)
			_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
			require.NoError(t, err)
			if prepared {
				f.reconcile(t, ctx)
				firmware.rejectRevision = true
			}
			firmware.snapshot.Groups[0].Pools[0].Username = "new-host"
			if prepared {
				f.reconcile(t, ctx)
			}
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status)
			require.Empty(t, firmware.snapshot.Groups[0].MACs)
			review = reviewDefaultReturn(t, ctx, f, ids)
			_, err = f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
			require.NoError(t, err)
		})
	}
}

func TestDefaultReturnSupersedesCompletedClearAndEnablesDeactivation(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	// Only the selected inventory board exists; remove the simulated sibling.
	firmware.snapshot.Groups[1].MACs = firmware.snapshot.Groups[1].MACs[:1]
	var installation int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE hashboard_id=$1`, ids[0]).Scan(&installation))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state,target_kind,target_id,reviewed_binding) VALUES($1,$2,7,$3,'ownership','completed','hashboard_installation',$4,'{"board":{"mac":"02:00:00:00:00:01"}}')`, f.target.OrganizationID, f.target.DeviceID, ids[0], installation)
	require.NoError(t, err)
	review := reviewDefaultReturn(t, ctx, f, ids)
	_, err = f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
	require.NoError(t, err)
	for range 5 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	cleared, err := f.service.queries.ListClearedBoardPoolIntents(ctx, sqlc.ListClearedBoardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, cleared, "later workflows must not move the board back to its cleared group")
	state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.Empty(t, state.DeactivationBlockedReason)
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 3 {
		f.reconcile(t, ctx)
	}
	require.False(t, firmware.snapshot.Enabled)
	require.Equal(t, statusComplete, f.state(t).Status)
}

func TestDefaultReturnContainerReviewSelectsOnlyUnallocatedBoards(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	var second, client int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&second))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) SELECT $1,module_id,2,now(),now(),'{"macAddress":"02:00:00:00:00:99"}' FROM hashboard_installation WHERE hashboard_id=$2`, second, ids[0])
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,2,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 ON CONFLICT(module_id,position) DO UPDATE SET hashboard_id=EXCLUDED.hashboard_id,occupancy='occupied',observed_at=now()`, second)
	require.NoError(t, err)
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&client))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, second, f.target.DeviceID, client)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	review := reviewDefaultReturn(t, ctx, f, nil)
	require.Len(t, review.Boards, 1)
	require.Equal(t, ids[0], review.Boards[0].HashboardID)
	_, err = f.service.ReviewReturnToDefault(ctx, f.target.DeviceIdentifier, []int64{ids[0], second})
	require.ErrorContains(t, err, "Recall the selected boards")
	// Completed/allocated history does not accidentally broaden the selection.
	require.Equal(t, []string{"02:00:00:00:00:01", "02:00:00:00:00:99"}, firmware.snapshot.Groups[1].MACs)
}

func TestDefaultReturnContainerReviewRequiresInventoryForUnknownGroupMembers(t *testing.T) {
	ctx, f, _, _ := newDefaultReturnFixture(t)
	_, err := f.service.ReviewReturnToDefault(ctx, f.target.DeviceIdentifier, nil)
	require.ErrorContains(t, err, "no current installation")
}

func TestDefaultReturnPreservesNewPoolPolicyBeforeOrAfterTransfer(t *testing.T) {
	for _, transferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "before transfer", true: "after transfer"}[transferred], func(t *testing.T) {
			ctx, f, _, ids := newDefaultReturnFixture(t)
			review := reviewDefaultReturn(t, ctx, f, ids)
			_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
			require.NoError(t, err)
			if transferred {
				for range 3 {
					f.reconcile(t, ctx)
				}
			}
			var policy uuid.UUID
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, ids[0]).Scan(&policy))
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status)
			require.Contains(t, f.state(t).Message, "pool policy changed")
			var status string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT state FROM pool_lifecycle_transition WHERE transition_id=$1`, policy).Scan(&status))
			require.Equal(t, "pending", status, "a new pool request must survive the earlier Default move")
		})
	}
}

func TestDefaultReturnKeepsStoppedDefaultStopped(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	firmware.snapshot.Groups[0].Mining = false
	review := reviewDefaultReturn(t, ctx, f, ids)
	require.False(t, review.Default.Mining)
	_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
	require.NoError(t, err)
	for range 4 {
		f.reconcile(t, ctx)
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	require.False(t, firmware.snapshot.Groups[0].Mining)
	for _, call := range firmware.calls {
		require.NotEqual(t, "start", call.Action)
	}
}

func TestDefaultReturnRetiresInvalidUnsentStep(t *testing.T) {
	for _, change := range []string{"installation", "pool policy", "stale inventory", "missing inventory"} {
		t.Run(change, func(t *testing.T) {
			ctx, f, firmware, ids := newDefaultReturnFixture(t)
			review := reviewDefaultReturn(t, ctx, f, ids)
			_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
			require.NoError(t, err)
			f.reconcile(t, ctx)
			prepared := f.state(t)
			require.True(t, prepared.Step.Valid)
			require.Empty(t, firmware.receipts)
			switch change {
			case "installation":
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{"macAddress":"02:00:00:00:00:02"}' WHERE hashboard_id=$1`, ids[0])
			case "pool policy":
				_, err = f.conn.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership')`, f.target.OrganizationID, f.target.DeviceID, ids[0])
			case "stale inventory":
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now()-INTERVAL '3 minutes' WHERE hashboard_id=$1`, ids[0])
			case "missing inventory":
				_, err = f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE hashboard_id=$1`, ids[0])
			}
			require.NoError(t, err)
			firmware.calls = nil
			f.reconcile(t, ctx)
			after := f.state(t)
			var attemptStatus string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, prepared.AttemptID.UUID).Scan(&attemptStatus))
			if change == "stale inventory" || change == "missing inventory" {
				require.Equal(t, "blocked", after.Status)
				require.Equal(t, prepared.Step, after.Step)
				require.Equal(t, "running", attemptStatus)
			} else {
				require.Equal(t, "failed", after.Status)
				require.False(t, after.Step.Valid)
				require.Equal(t, "complete", attemptStatus)
			}
			for _, call := range firmware.calls {
				require.True(t, call.ReadOnly())
			}
			require.Empty(t, firmware.receipts)
		})
	}
}

func TestDefaultReturnKeepsAdmissionAfterInvalidSavedSelection(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	review := reviewDefaultReturn(t, ctx, f, ids)
	_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
	require.NoError(t, err)
	before := f.state(t)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET default_return='{"boards":"invalid"}' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	firmware.calls = nil
	err = f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
	var decodeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &decodeErr)
	after := f.state(t)
	require.Equal(t, before.Status, after.Status, "invalid saved data must not be recorded as a board change or stale inventory")
	require.Equal(t, before.AttemptID, after.AttemptID)
	var status string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, before.AttemptID.UUID).Scan(&status))
	require.Equal(t, "running", status)
	for _, call := range firmware.calls {
		require.True(t, call.ReadOnly())
	}
	require.Empty(t, firmware.receipts)
}

func TestDefaultReturnKeepsInvalidStepWithUnknownDelivery(t *testing.T) {
	for _, delivery := range []string{"dispatched", "missing history"} {
		t.Run(delivery, func(t *testing.T) {
			ctx, f, firmware, ids := newDefaultReturnFixture(t)
			review := reviewDefaultReturn(t, ctx, f, ids)
			_, err := f.service.ReturnToDefault(ctx, f.target.DeviceIdentifier, review.Token, uuid.NewString(), ids)
			require.NoError(t, err)
			f.reconcile(t, ctx)
			if delivery == "dispatched" {
				firmware.loseReply = true
				f.reconcile(t, ctx)
				require.NotEmpty(t, firmware.receipts)
			} else {
				_, err = f.conn.ExecContext(ctx, `DELETE FROM mining_group_step_dispatch WHERE step_key=$1`, f.state(t).StepKey.UUID)
				require.NoError(t, err)
			}
			before := f.state(t)
			_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{"macAddress":"02:00:00:00:00:02"}' WHERE hashboard_id=$1`, ids[0])
			require.NoError(t, err)
			firmware.calls = nil
			f.reconcile(t, ctx)
			after := f.state(t)
			require.Equal(t, "blocked", after.Status)
			require.Equal(t, before.Step, after.Step)
			require.Equal(t, before.StepKey, after.StepKey)
			require.Empty(t, firmware.calls)
			var attemptStatus string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, before.AttemptID.UUID).Scan(&attemptStatus))
			require.Equal(t, "running", attemptStatus)
		})
	}
}

func TestDefaultReturnReviewDoesNotClaimCompletionAfterReadFailure(t *testing.T) {
	ctx, f, firmware, ids := newDefaultReturnFixture(t)
	firmware.snapshot.Groups[0].MACs = []string{"02:00:00:00:00:01"}
	firmware.snapshot.Groups[1].MACs = nil
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	firmware.observeErr = true
	_, err = f.service.ReviewReturnToDefault(ctx, "", ids)
	require.ErrorContains(t, err, "refresh its mining-group state")
}

func TestDefaultReturnReviewUsesMiningAssetNames(t *testing.T) {
	ctx, f, _, ids := newDefaultReturnFixture(t)
	review := reviewDefaultReturn(t, ctx, f, ids)
	require.Equal(t, fmt.Sprintf("Hashboard %d", ids[0]), review.BoardNames[ids[0]])
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES($1,'mac','02:00:00:00:00:01',$2)`, f.target.OrganizationID, ids[0])
	require.NoError(t, err)
	review = reviewDefaultReturn(t, ctx, f, ids)
	require.Equal(t, "Hashboard 02:00:00:00:00:01", review.BoardNames[ids[0]])
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES($1,'serial','CT1-0001',$2)`, f.target.OrganizationID, ids[0])
	require.NoError(t, err)
	review = reviewDefaultReturn(t, ctx, f, ids)
	require.Equal(t, "Hashboard CT1-0001", review.BoardNames[ids[0]])
}
