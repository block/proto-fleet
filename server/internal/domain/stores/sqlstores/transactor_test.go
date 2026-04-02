package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"

	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// setupTransactorTest creates the test infrastructure needed for transactor tests
func setupTransactorTest(t *testing.T) (*sqlstores.SQLTransactor, *sqlstores.SQLPoolStore) {
	t.Helper()

	db := testutil.GetTestDB(t)
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	encryptService, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: config.ServiceMasterKey})
	require.NoError(t, err)

	transactor := sqlstores.NewSQLTransactor(db)
	poolStore := sqlstores.NewSQLPoolStore(db, encryptService)

	_, err = db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org-1', 'Test Organization 1', 'dummy-key-for-testing') ON CONFLICT DO NOTHING`)
	require.NoError(t, err, "Failed to create test organization")

	return transactor, poolStore
}

func TestWithTransaction_OuterRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctx := t.Context()
	transactor, poolStore := setupTransactorTest(t)

	testPoolConfig := &poolspb.PoolConfig{
		Url:      "stratum+tcp://test.pool.com:3333",
		Username: "test.worker",
		Password: wrapperspb.String("test123"),
	}

	// Act
	var poolID int64
	err := transactor.RunInTx(ctx, func(ctx context.Context) error {
		innerErr := transactor.RunInTx(ctx, func(ctx context.Context) error {
			var createErr error
			poolID, createErr = poolStore.CreatePool(ctx, testPoolConfig, 1)
			return createErr
		})
		require.NoError(t, innerErr)

		return sql.ErrTxDone
	})

	// Assert
	require.Error(t, err)

	_, err = poolStore.GetPool(ctx, 1, poolID)
	require.Error(t, err, "Pool should not exist after transaction rollback")
}

func TestWithTransaction_InnerRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctx := t.Context()
	transactor, poolStore := setupTransactorTest(t)

	testPoolConfig := &poolspb.PoolConfig{
		Url:      "stratum+tcp://test.pool.com:3333",
		Username: "test.worker",
		Password: wrapperspb.String("test123"),
	}

	// Act
	var poolID int64
	err := transactor.RunInTx(ctx, func(ctx context.Context) error {
		var createErr error
		poolID, createErr = poolStore.CreatePool(ctx, testPoolConfig, 1)
		require.NoError(t, createErr)
		return transactor.RunInTx(ctx, func(_ context.Context) error {
			return sql.ErrTxDone
		})
	})

	// Assert
	require.Error(t, err)

	_, err = poolStore.GetPool(ctx, 1, poolID)
	require.Error(t, err, "Pool should not exist after transaction rollback")
}

func TestNestedTransactions_DatabaseLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctx := t.Context()
	transactor, poolStore := setupTransactorTest(t)

	outerPoolConfig := &poolspb.PoolConfig{
		Url:      "stratum+tcp://outer.pool.com:3333",
		Username: "outer.worker",
		Password: wrapperspb.String("outer123"),
	}

	innerPoolConfig := &poolspb.PoolConfig{
		Url:      "stratum+tcp://inner.pool.com:3333",
		Username: "inner.worker",
		Password: wrapperspb.String("inner123"),
	}

	// Act - This test will deadlock if nested transactions are created at DB level
	var outerPoolID, innerPoolID int64
	err := transactor.RunInTx(ctx, func(outerCtx context.Context) error {
		var outerErr error
		outerPoolID, outerErr = poolStore.CreatePool(outerCtx, outerPoolConfig, 1)
		require.NoError(t, outerErr)

		// Attempt a nested transaction
		return transactor.RunInTx(outerCtx, func(innerCtx context.Context) error {
			var innerErr error
			innerPoolID, innerErr = poolStore.CreatePool(innerCtx, innerPoolConfig, 1)
			require.NoError(t, innerErr)

			// If these are truly separate transactions at DB level, this would likely cause a deadlock
			// because the inner transaction would be waiting for a lock held by the outer transaction

			// Read the outer pool from the inner transaction context
			// This should succeed if there's proper transaction handling (same transaction)
			_, getErr := poolStore.GetPool(innerCtx, 1, outerPoolID)
			return getErr
		})
	})

	// Assert
	require.NoError(t, err, "Nested transaction operations should complete without deadlock")

	// Verify both pools were created in the same transaction
	// by checking if they both exist
	_, outerErr := poolStore.GetPool(ctx, 1, outerPoolID)
	require.NoError(t, outerErr)

	_, innerErr := poolStore.GetPool(ctx, 1, innerPoolID)
	require.NoError(t, innerErr)
}
