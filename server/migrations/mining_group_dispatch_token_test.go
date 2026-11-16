package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMiningGroupDispatchTokensAreUniqueAndRequired(t *testing.T) {
	conn, org, device := miningGroupRollbackFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	var err error
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
