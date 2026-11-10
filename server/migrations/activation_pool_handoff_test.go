package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActivationPoolHandoffRollback(t *testing.T) {
	for _, state := range []string{"empty", "pending", "completed"} {
		t.Run(state, func(t *testing.T) {
			conn, org, device := miningGroupRollbackFixture(t)
			ctx := t.Context()
			if state != "empty" {
				id := uuid.New()
				_, err := conn.ExecContext(ctx, `INSERT INTO mining_group_request(organization_id,request_id,device_id,fingerprint) VALUES($1,$2,$3,'activation')`, org, id, device)
				require.NoError(t, err)
				_, err = conn.ExecContext(ctx, `INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured,completed_at) VALUES($1,$2,$3,'{"evidence":"retain"}',CASE WHEN $4 THEN now() ELSE NULL END)`, org, id, device, state == "completed")
				require.NoError(t, err)
			}
			down, err := migrations.Current.ReadFile("current/001070_internal_activation_pool_handoff.down.sql")
			require.NoError(t, err)
			// A prepared statement proves the guard and DROP are atomic even without
			// an outer transaction supplied by the migration runner.
			rollback, err := conn.PrepareContext(ctx, string(down))
			require.NoError(t, err)
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			if state != "empty" {
				require.ErrorContains(t, err, "activation pool handoff")
				var evidence string
				var completed bool
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT captured->>'evidence',completed_at IS NOT NULL FROM mining_group_pool_handoff`).Scan(&evidence, &completed))
				require.Equal(t, "retain", evidence)
				require.Equal(t, state == "completed", completed)
				return
			}
			require.NoError(t, err)
			var absent bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('mining_group_pool_handoff') IS NULL`).Scan(&absent))
			require.True(t, absent)
			up, err := migrations.Current.ReadFile("current/001070_internal_activation_pool_handoff.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err)
		})
	}
}

func TestActivationPoolHandoffRollbackSeesConcurrentCapture(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	id := uuid.New()
	_, err = writer.ExecContext(ctx, `INSERT INTO mining_group_request(organization_id,request_id,device_id,fingerprint) VALUES($1,$2,$3,'activation')`, org, id, device)
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, `INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured) VALUES($1,$2,$3,'{"evidence":"concurrent"}')`, org, id, device)
	require.NoError(t, err)
	downConn, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer downConn.Close()
	var pid int
	require.NoError(t, downConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	down, err := migrations.Current.ReadFile("current/001070_internal_activation_pool_handoff.down.sql")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := downConn.ExecContext(ctx, string(down)); done <- err }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted AND mode='AccessExclusiveLock')`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	require.ErrorContains(t, <-done, "activation pool handoff")
	var evidence string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT captured->>'evidence' FROM mining_group_pool_handoff`).Scan(&evidence))
	require.Equal(t, "concurrent", evidence)
}

func rollbackEmptyActivationPoolHandoff(t *testing.T, conn *sql.DB) {
	t.Helper()
	down, err := migrations.Current.ReadFile("current/001070_internal_activation_pool_handoff.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
}
