package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const miningGroupDispatchIdentityMigration = "current/001085_internal_mining_group_dispatch_token"

func TestMiningGroupDispatchTokenUpgradePreservesDeliveryEvidence(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	ctx := t.Context()
	down, err := migrations.Current.ReadFile(miningGroupDispatchIdentityMigration + ".down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	q := sqlc.New(conn)
	require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{device}))
	attempt := uuid.New()
	require.NoError(t, q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{
		AttemptID: attempt, DeviceID: device, OrganizationID: org, OwnerInstanceID: uuid.New(),
		IdentityKeys: []string{"dispatch-token-upgrade"}, OperationKind: "mutation", CommandType: "MiningGroups",
	}))
	keys := []uuid.UUID{uuid.New(), uuid.New()}
	for i, key := range keys {
		_, err = conn.ExecContext(ctx, `INSERT INTO mining_group_step_dispatch(step_key,attempt_id,dispatch_started) VALUES($1,$2,$3)`, key, attempt, i == 1)
		require.NoError(t, err)
	}
	up, err := migrations.Current.ReadFile(miningGroupDispatchIdentityMigration + ".up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var tokens []uuid.UUID
	for i, key := range keys {
		var token, retainedAttempt uuid.UUID
		var started bool
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT dispatch_token,attempt_id,dispatch_started FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&token, &retainedAttempt, &started))
		require.NotEqual(t, uuid.Nil, token)
		require.Equal(t, attempt, retainedAttempt)
		require.Equal(t, i == 1, started, "adding dispatch identities must not invent or erase delivery evidence")
		tokens = append(tokens, token)
	}
	require.NotEqual(t, tokens[0], tokens[1], "each saved step needs its own dispatch identity")
	key := uuid.New()
	_, err = conn.ExecContext(ctx, `INSERT INTO mining_group_step_dispatch(step_key,attempt_id) VALUES($1,$2)`, key, attempt)
	require.NoError(t, err)
	var token uuid.UUID
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT dispatch_token FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&token))
	require.NotEqual(t, uuid.Nil, token, "new dispatch records also need a generated identity")
	_, err = conn.ExecContext(ctx, `INSERT INTO mining_group_step_dispatch(step_key,attempt_id,dispatch_token) VALUES($1,$2,NULL)`, uuid.New(), attempt)
	require.ErrorContains(t, err, "dispatch_token", "a dispatch record must not lose its identity")
}

func TestMiningGroupDispatchTokenRollbackPreservesSavedRequests(t *testing.T) {
	for _, status := range []string{"pending", "applying", "blocked"} {
		t.Run(status, func(t *testing.T) {
			conn, org, device := miningGroupRollbackFixture(t)
			ctx := t.Context()
			attempt, key, token := uuid.New(), uuid.New(), uuid.New()
			q := sqlc.New(conn)
			require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{device}))
			require.NoError(t, q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{
				AttemptID: attempt, DeviceID: device, OrganizationID: org, OwnerInstanceID: uuid.New(),
				IdentityKeys: []string{"dispatch-token-rollback"}, OperationKind: "mutation", CommandType: "MiningGroups",
			}))
			_, err := conn.ExecContext(ctx, `INSERT INTO mining_group_step_dispatch(step_key,attempt_id,dispatch_started,dispatch_token) VALUES($1,$2,true,$3)`, key, attempt, token)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$1,step_key=$2 WHERE device_id=$3`, status, key, device)
			require.NoError(t, err)
			down, err := migrations.Current.ReadFile(miningGroupDispatchIdentityMigration + ".down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(down))
			require.ErrorContains(t, err, "Settle saved mining-group requests before removing dispatch tokens")
			var retainedToken uuid.UUID
			var started bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT dispatch_token,dispatch_started FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&retainedToken, &started))
			require.Equal(t, token, retainedToken, "refused downgrade must preserve the exact active dispatch identity")
			require.True(t, started)
			_, err = conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, device)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(down))
			require.NoError(t, err, "settled work permits downgrade")
			var tokenColumn bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='mining_group_step_dispatch' AND column_name='dispatch_token')`).Scan(&tokenColumn))
			require.False(t, tokenColumn)
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT dispatch_started FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&started))
			require.True(t, started, "downgrade preserves delivery evidence")
			up, err := migrations.Current.ReadFile(miningGroupDispatchIdentityMigration + ".up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err)
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT dispatch_token,dispatch_started FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&retainedToken, &started))
			require.NotEqual(t, uuid.Nil, retainedToken)
			require.NotEqual(t, token, retainedToken, "recreated dispatch identities must invalidate old tokens")
			require.True(t, started)
		})
	}
}
