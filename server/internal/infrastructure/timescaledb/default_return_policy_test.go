package timescaledb

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDefaultReturnBlocksOnlyRetryEligibleFailedClears(t *testing.T) {
	for _, tc := range []struct {
		name                                                    string
		missingAttempt, running, wrongCommand, delivered, board bool
		pending                                                 bool
	}{
		{name: "retry eligible", pending: true},
		{name: "missing admission", missingAttempt: true},
		{name: "unfinished admission", running: true},
		{name: "another command", wrongCommand: true},
		{name: "delivery evidence", delivered: true},
		{name: "hashboard target", board: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			ctx := t.Context()
			q := sqlc.New(r.db)
			require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{r.deviceID}))
			var attempt uuid.NullUUID
			if !tc.missingAttempt {
				id := uuid.New()
				command := "UpdateMiningPools"
				if tc.wrongCommand {
					command = "Reboot"
				}
				require.NoError(t, q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{
					AttemptID: id, OrganizationID: r.orgID, DeviceID: r.deviceID, OwnerInstanceID: uuid.New(),
					IdentityKeys: []string{"failed-clear"}, OperationKind: "mutation", CommandType: command,
				}))
				attempt = uuid.NullUUID{UUID: id, Valid: true}
			}
			kind := "device"
			if tc.board {
				kind = "hashboard_installation"
			}
			var transition uuid.UUID
			// Admission binds only applying work. Record failure after that binding,
			// as the direct executor does when it proves the call was not dispatched.
			require.NoError(t, r.db.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state,target_kind,target_id,physical_attempt_id)
    VALUES($1,$2,5,$2,'ownership','applying',$3,$2,$4) RETURNING transition_id`, r.orgID, r.deviceID, kind, attempt).Scan(&transition))
			if attempt.Valid && !tc.running {
				outcome := []byte(`{}`)
				if tc.delivered {
					outcome = []byte(`{"dispatched":true}`)
				}
				_, err := q.FinishDeviceWriteAttempt(ctx, sqlc.FinishDeviceWriteAttemptParams{AttemptID: attempt.UUID, Status: "complete", WriteOutcome: outcome})
				require.NoError(t, err)
			}
			_, err := r.db.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='failed',result_code='clear_not_dispatched' WHERE transition_id=$1`, transition)
			require.NoError(t, err)
			pending, err := q.HasPendingDefaultReturnPoolPolicy(ctx, sqlc.HasPendingDefaultReturnPoolPolicyParams{OrganizationID: r.orgID, DeviceID: r.deviceID})
			require.NoError(t, err)
			require.True(t, pending.Valid)
			require.Equal(t, tc.pending, pending.Bool)
			blocker, err := q.GetMiningGroupDeactivationBlocker(ctx, sqlc.GetMiningGroupDeactivationBlockerParams{OrganizationID: r.orgID, DeviceID: r.deviceID})
			require.NoError(t, err)
			require.Equal(t, tc.pending, blocker != "")
			policies, err := q.ListPendingPoolLifecycleTransitions(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.pending, len(policies) > 0)
			require.NoError(t, q.RetryUndispatchedPoolLifecycleRemoval(ctx, transition))
			var state string
			var retained sql.NullString
			require.NoError(t, r.db.QueryRowContext(ctx, `SELECT state,physical_attempt_id::text FROM pool_lifecycle_transition WHERE transition_id=$1`, transition).Scan(&state, &retained))
			if tc.pending {
				require.Equal(t, "ready", state)
				require.False(t, retained.Valid)
			} else {
				require.Equal(t, "failed", state)
				require.Equal(t, !tc.missingAttempt, retained.Valid)
			}
		})
	}
}
