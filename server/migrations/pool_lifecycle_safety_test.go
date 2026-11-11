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

const poolLifecycleMigration = "current/001074_internal_default_pool_lifecycle"

func TestPoolLifecycleRollbackSeesConcurrentHistory(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	var writerPID int
	require.NoError(t, writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
	_, err = writer.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,5,$2,'ownership')`, org, device)
	require.NoError(t, err)
	downConn, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer downConn.Close()
	var pid int
	require.NoError(t, downConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	down, err := migrations.Current.ReadFile(poolLifecycleMigration + ".down.sql")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := downConn.ExecContext(ctx, string(down)); done <- err }()
	// An unfenced guard sees no committed history and waits only at later DDL.
	// Releasing the writer must make the guard refuse, not discard its new row.
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, writerPID, pid).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	require.ErrorContains(t, <-done, "retain pool lifecycle history")
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_lifecycle_transition`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestPoolLifecycleRollbackIsAtomicAndPreservesHistory(t *testing.T) {
	for _, state := range []string{"empty", "pending", "completed", "superseded"} {
		t.Run(state, func(t *testing.T) {
			conn, org, device := miningGroupRollbackFixture(t)
			ctx := t.Context()
			if state != "empty" {
				_, err := conn.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state) VALUES($1,$2,5,$2,'ownership',$3)`, org, device, state)
				require.NoError(t, err)
			}
			down, err := migrations.Current.ReadFile(poolLifecycleMigration + ".down.sql")
			require.NoError(t, err)
			// Preparing the migration requires one statement, so lock, guard and DDL
			// cannot accidentally rely on the runner supplying an outer transaction.
			rollback, err := conn.PrepareContext(ctx, string(down))
			require.NoError(t, err)
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			if state != "empty" {
				require.ErrorContains(t, err, "retain pool lifecycle history")
				var retained string
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT state FROM pool_lifecycle_transition`).Scan(&retained))
				require.Equal(t, state, retained)
				var triggers int
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger WHERE tgname IN ('device_pairing_pool_transition','hosted_allocation_pool_transition','hashboard_removed_pool_transitions','hashboard_reinstalled_pool_transition')`).Scan(&triggers))
				require.Equal(t, 4, triggers, "refused rollback must preserve every lifecycle producer")
				return
			}
			require.NoError(t, err)
			var absent bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('pool_lifecycle_transition') IS NULL AND to_regclass('pool_pairing_cycle') IS NULL AND NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='pool_configuration' AND column_name='is_default')`).Scan(&absent))
			require.True(t, absent)
			up, err := migrations.Current.ReadFile(poolLifecycleMigration + ".up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err)
		})
	}
}

func TestPoolLifecycleAttemptReferenceIndexAndCleanup(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{device}))
	referenced, unreferenced := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{referenced, unreferenced} {
		require.NoError(t, q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{
			AttemptID: id, DeviceID: device, OrganizationID: org, OwnerInstanceID: uuid.New(), IdentityKeys: []string{"test-device"}, OperationKind: "mutation", CommandType: "UpdateMiningPools",
		}))
	}
	// Most retained history has no direct physical attempt. A small non-null
	// subset still needs a selective lookup without scanning all retained rows.
	_, err := conn.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state,physical_attempt_id)
 SELECT $1,$2,5,$2,'ownership',CASE WHEN n<=20 THEN 'applying' ELSE 'completed' END,
 CASE WHEN n<=20 THEN $3::uuid ELSE NULL END FROM generate_series(1,20000) n`, org, device, referenced)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `ANALYZE pool_lifecycle_transition`)
	require.NoError(t, err)
	var plan string
	require.NoError(t, conn.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) SELECT 1 FROM pool_lifecycle_transition WHERE physical_attempt_id=$1`, unreferenced).Scan(&plan))
	require.Contains(t, plan, "idx_pool_lifecycle_physical_attempt", "the cleanup reference check should use its index without disabling sequential scans")
	t.Logf("attempt-reference plan: %s", plan)
	for _, id := range []uuid.UUID{referenced, unreferenced} {
		_, err := q.FinishDeviceWriteAttempt(ctx, sqlc.FinishDeviceWriteAttemptParams{Status: "complete", AttemptID: id, WriteOutcome: []byte("{}")})
		require.NoError(t, err)
		require.NoError(t, q.DeleteUndispatchedDirectAttempt(ctx, sqlc.DeleteUndispatchedDirectAttemptParams{AttemptID: id, DeviceID: device, OrganizationID: org}))
		var exists bool
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device_write_attempt WHERE attempt_id=$1)`, id).Scan(&exists))
		require.Equal(t, id == referenced, exists, "cleanup must retain referenced physical evidence")
	}
}
