package sqlstores_test

import (
	"testing"

	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestSQLStores_AllowKeyReuseAfterSoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := t.Context()
	poolStore, userStore := setupSoftDeleteUniquenessStores(t)

	t.Run("pool key", func(t *testing.T) {
		config := &poolspb.PoolConfig{
			PoolName: "Reusable Pool",
			Url:      "stratum+tcp://reuse.pool.example.com:3333",
			Username: "wallet",
			Password: wrapperspb.String("secret"),
		}

		firstID, err := poolStore.CreatePool(ctx, config, 1)
		require.NoError(t, err)

		_, err = poolStore.CreatePool(ctx, config, 1)
		requireUniqueViolationOn(t, err, "uk_pool_org_url_username")

		require.NoError(t, poolStore.SoftDeletePool(ctx, 1, firstID))

		secondID, err := poolStore.CreatePool(ctx, config, 1)
		require.NoError(t, err)
		require.NotEqual(t, firstID, secondID)
	})

	t.Run("username", func(t *testing.T) {
		firstID, err := userStore.CreateUser(ctx, "external-user-1", "reuse@example.com", "hash", false)
		require.NoError(t, err)

		_, err = userStore.CreateUser(ctx, "external-user-2", "reuse@example.com", "hash", false)
		requireUniqueViolationOn(t, err, "uq_user_username")

		require.NoError(t, userStore.SoftDeleteUser(ctx, firstID))

		secondID, err := userStore.CreateUser(ctx, "external-user-2", "reuse@example.com", "hash", false)
		require.NoError(t, err)
		require.NotEqual(t, firstID, secondID)
	})
}

func setupSoftDeleteUniquenessStores(t *testing.T) (*sqlstores.SQLPoolStore, *sqlstores.SQLUserStore) {
	t.Helper()

	db := testutil.GetTestDB(t)
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)

	encryptService, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: config.ServiceMasterKey})
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (1, 'test-org-1', 'Test Organization 1') ON CONFLICT DO NOTHING`)
	require.NoError(t, err, "Failed to create test organization")

	return sqlstores.NewSQLPoolStore(db, encryptService), sqlstores.NewSQLUserStore(db)
}

func requireUniqueViolationOn(t *testing.T, err error, constraintName string) {
	t.Helper()

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, db.PGUniqueViolation, pgErr.Code)
	require.Equal(t, constraintName, pgErr.ConstraintName)
}
