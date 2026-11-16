package timescaledb

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReturningBoardTelemetryDefersPoolPolicyDuringDeactivation(t *testing.T) {
	for _, status := range []string{"pending", "applying", "blocked"} {
		t.Run(status, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
			old := r.boardID(t, "old")
			_, err := r.db.ExecContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason)
			 VALUES($1,$2,7,$3,'ownership')`, r.orgID, r.deviceID, old)
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "")))
			_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status)
			 VALUES($1,$2,'deactivate',$3) ON CONFLICT(device_id) DO UPDATE SET operation_kind='deactivate',status=$3`, r.deviceID, r.orgID, status)
			require.NoError(t, err)

			returned := replacementSnapshot(r.base.Add(2*time.Second), "old")
			require.NoError(t, r.store(t, returned), "automatic policy must not roll back valid returning-board telemetry")
			var installation int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, old).Scan(&installation))
			var policyID uuid.UUID
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition
			 WHERE asset_kind=7 AND asset_id=$1 AND state='deferred'`, old).Scan(&policyID))
			q := sqlc.New(r.db)
			require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
			policy, err := q.GetPoolLifecycleTransition(t.Context(), policyID)
			require.NoError(t, err)
			require.Equal(t, "deferred", policy.State)
			pending, err := q.ListPendingPoolLifecycleTransitions(t.Context())
			require.NoError(t, err)
			require.Empty(t, pending, "deferred policy must not reach device execution")

			_, err = r.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, r.deviceID)
			require.NoError(t, err)
			require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
			policy, err = q.GetPoolLifecycleTransition(t.Context(), policyID)
			require.NoError(t, err)
			require.Equal(t, "pending", policy.State)
			pending, err = q.ListPendingPoolLifecycleTransitions(t.Context())
			require.NoError(t, err)
			require.Len(t, pending, 1)
			require.Equal(t, policyID, pending[0].TransitionID, "the original automatic policy resumes after settlement")
		})
	}
}

func TestDeferredPoolPolicyRetiresWhenReturningBoardLeavesAgain(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
	old := r.boardID(t, "old")
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status)
	 VALUES($1,$2,'deactivate','blocked')`, r.deviceID, r.orgID)
	require.NoError(t, err)
	var policyID uuid.UUID
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason)
	 VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, r.orgID, r.deviceID, old).Scan(&policyID))
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "")))
	q := sqlc.New(r.db)
	require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
	policy, err := q.GetPoolLifecycleTransition(t.Context(), policyID)
	require.NoError(t, err)
	require.Equal(t, "superseded", policy.State)
	require.Equal(t, "board_removed", policy.ResultCode)
}

func TestPairingDuringDeactivationDefersPolicyAndUnpairRetiresIt(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status)
	 VALUES($1,$2,'deactivate','blocked')`, r.deviceID, r.orgID)
	require.NoError(t, err)
	q := sqlc.New(r.db)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: r.deviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err, "pool policy must not roll back pairing")
	var policyID uuid.UUID
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND state='deferred'`, r.deviceID).Scan(&policyID))
	require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
	policy, err := q.GetPoolLifecycleTransition(t.Context(), policyID)
	require.NoError(t, err)
	require.Equal(t, "deferred", policy.State)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: r.deviceID, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
	require.NoError(t, err)
	require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
	policy, err = q.GetPoolLifecycleTransition(t.Context(), policyID)
	require.NoError(t, err)
	require.Equal(t, "superseded", policy.State)
	require.Equal(t, "pairing_cycle_ended", policy.ResultCode)
}
