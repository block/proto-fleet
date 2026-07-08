package sqlstores_test

import (
	"testing"

	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestSQLPoolStore_AllowsPoolKeyReuseAfterSoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := t.Context()
	_, poolStore := setupTransactorTest(t)
	config := &poolspb.PoolConfig{
		PoolName: "Reusable Pool",
		Url:      "stratum+tcp://reuse.pool.example.com:3333",
		Username: "wallet",
		Password: wrapperspb.String("secret"),
	}

	firstID, err := poolStore.CreatePool(ctx, config, 1)
	require.NoError(t, err)

	_, err = poolStore.CreatePool(ctx, config, 1)
	require.Error(t, err, "duplicate live pool keys must still be rejected")

	require.NoError(t, poolStore.SoftDeletePool(ctx, 1, firstID))

	secondID, err := poolStore.CreatePool(ctx, config, 1)
	require.NoError(t, err)
	require.NotEqual(t, firstID, secondID)
}

func TestSQLUserStore_AllowsUsernameReuseAfterSoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := t.Context()
	db := testutil.GetTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)

	firstID, err := userStore.CreateUser(ctx, "external-user-1", "reuse@example.com", "hash", false)
	require.NoError(t, err)

	_, err = userStore.CreateUser(ctx, "external-user-2", "reuse@example.com", "hash", false)
	require.Error(t, err, "duplicate live usernames must still be rejected")

	require.NoError(t, userStore.SoftDeleteUser(ctx, firstID))

	secondID, err := userStore.CreateUser(ctx, "external-user-2", "reuse@example.com", "hash", false)
	require.NoError(t, err)
	require.NotEqual(t, firstID, secondID)
}
