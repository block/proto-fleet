package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMiningGroupDispatchRollbackPreservesSavedRequests(t *testing.T) {
	for _, status := range []string{"pending", "applying", "blocked", "complete"} {
		t.Run(status, func(t *testing.T) {
			conn, org, device := miningGroupRollbackFixture(t)
			key, attempt, token := uuid.New(), uuid.New(), uuid.New()
			q := sqlc.New(conn)
			require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{device}))
			require.NoError(t, q.CreateDeviceWriteAttempt(t.Context(), sqlc.CreateDeviceWriteAttemptParams{
				AttemptID: attempt, OrganizationID: org, DeviceID: device, OwnerInstanceID: uuid.New(),
				IdentityKeys: []string{"dispatch-rollback"}, OperationKind: "mutation", CommandType: "MiningGroups",
			}))
			_, err := conn.ExecContext(t.Context(), `INSERT INTO mining_group_step_dispatch(step_key,attempt_id,dispatch_started,dispatch_token) VALUES($1,$2,true,$3)`, key, attempt, token)
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status=$1,step_key=$2 WHERE device_id=$3`, status, key, device)
			require.NoError(t, err)
			down, err := migrations.Current.ReadFile("current/001084_internal_mining_group_step_dispatch.down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(down))
			if status != "complete" {
				require.ErrorContains(t, err, "Settle saved mining-group requests")
				var exists bool
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('mining_group_step_dispatch') IS NOT NULL`).Scan(&exists))
				require.True(t, exists)
				var retainedToken uuid.UUID
				var started bool
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT dispatch_token,dispatch_started FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&retainedToken, &started))
				require.Equal(t, token, retainedToken, "refused rollback must preserve the saved invocation")
				require.True(t, started, "refused rollback must preserve delivery evidence")
				return
			}
			require.NoError(t, err)
			up, err := migrations.Current.ReadFile("current/001084_internal_mining_group_step_dispatch.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(up))
			require.NoError(t, err)
			var records int
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&records))
			require.Zero(t, records, "upgrade must not infer delivery for an older request")
		})
	}
}
