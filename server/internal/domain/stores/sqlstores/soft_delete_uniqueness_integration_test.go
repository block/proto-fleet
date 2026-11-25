package sqlstores_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestSQLStores_AllowKeyReuseAfterSoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := t.Context()
	queries, userStore := setupSoftDeleteUniquenessStores(t)

	t.Run("pool key", func(t *testing.T) {
		config := sqlc.CreatePoolParams{
			OrgID:     1,
			CreatedAt: time.Now(),
			PoolName:  "Reusable Pool",
			Url:       "stratum+tcp://reuse.pool.example.com:3333",
			Username:  "wallet",
		}

		firstID, err := queries.CreatePool(ctx, config)
		require.NoError(t, err)

		_, err = queries.CreatePool(ctx, config)
		requireUniqueViolationOn(t, err, "uk_pool_org_url_username")

		require.NoError(t, queries.SoftDeletePool(ctx, sqlc.SoftDeletePoolParams{OrgID: 1, ID: firstID}))

		secondID, err := queries.CreatePool(ctx, config)
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

func setupSoftDeleteUniquenessStores(t *testing.T) (*sqlc.Queries, *sqlstores.SQLUserStore) {
	t.Helper()

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (1, 'test-org-1', 'Test Organization 1') ON CONFLICT DO NOTHING`)
	require.NoError(t, err, "Failed to create test organization")

	return sqlc.New(db), sqlstores.NewSQLUserStore(db)
}

func requireUniqueViolationOn(t *testing.T, err error, constraintName string) {
	t.Helper()

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, db.PGUniqueViolation, pgErr.Code)
	require.Equal(t, constraintName, pgErr.ConstraintName)
}
