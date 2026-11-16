package migrations_test

import (
	"context"
	"testing"
	"time"

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

func TestMiningGroupDispatchRollbackSeesConcurrentSavedStep(t *testing.T) {
	for _, started := range []bool{false, true} {
		name := "unsent"
		if started {
			name = "dispatch started"
		}
		t.Run(name, func(t *testing.T) {
			conn, org, device := miningGroupRollbackFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			q := sqlc.New(conn)
			require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{device}))
			key, attempt, token := uuid.New(), uuid.New(), uuid.New()
			require.NoError(t, q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{
				AttemptID: attempt, OrganizationID: org, DeviceID: device, OwnerInstanceID: uuid.New(),
				IdentityKeys: []string{"concurrent-dispatch-rollback"}, OperationKind: "mutation", CommandType: "MiningGroups",
			}))
			writer, err := conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer writer.Rollback()
			var writerPID int
			require.NoError(t, writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
			_, err = writer.ExecContext(ctx, `INSERT INTO mining_group_step_dispatch(step_key,attempt_id,dispatch_started,dispatch_token) VALUES($1,$2,$3,$4)`, key, attempt, started, token)
			require.NoError(t, err)
			_, err = writer.ExecContext(ctx, `UPDATE container_mining_group_state SET status='applying',step_key=$1 WHERE device_id=$2`, key, device)
			require.NoError(t, err)
			downConn, err := conn.Conn(ctx)
			require.NoError(t, err)
			defer downConn.Close()
			var downPID int
			require.NoError(t, downConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
			down, err := migrations.Current.ReadFile("current/001084_internal_mining_group_step_dispatch.down.sql")
			require.NoError(t, err)
			// Preparing also requires the locks, guard, and removal to be one SQL
			// statement, independent of an outer migration transaction.
			rollback, err := downConn.PrepareContext(ctx, string(down))
			require.NoError(t, err)
			defer rollback.Close()
			done := make(chan error, 1)
			go func() { _, err := rollback.ExecContext(ctx); done <- err }()
			// The old guard sees the prior idle journal, then waits only at DROP.
			// The fenced guard waits first and must inspect the newly committed step.
			require.Eventually(t, func() bool {
				var waiting bool
				err := conn.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, writerPID, downPID).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			require.NoError(t, writer.Commit())
			require.ErrorContains(t, <-done, "Settle saved mining-group requests")
			var retainedKey, retainedToken, retainedAttempt uuid.UUID
			var retainedStarted bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT s.step_key,d.dispatch_token,d.attempt_id,d.dispatch_started
    FROM container_mining_group_state s JOIN mining_group_step_dispatch d ON d.step_key=s.step_key WHERE s.device_id=$1`, device).Scan(&retainedKey, &retainedToken, &retainedAttempt, &retainedStarted))
			require.Equal(t, key, retainedKey)
			require.Equal(t, token, retainedToken)
			require.Equal(t, attempt, retainedAttempt)
			require.Equal(t, started, retainedStarted, "refused downgrade must retain exact delivery evidence")
		})
	}
}
