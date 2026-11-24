package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// setupTransactorTest creates the test infrastructure needed for transactor tests
func setupTransactorTest(t *testing.T) *sqlstores.SQLTransactor {
	t.Helper()

	db := testutil.GetTestDB(t)
	transactor := sqlstores.NewSQLTransactor(db)

	_, err := db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (1, 'test-org-1', 'Test Organization 1') ON CONFLICT DO NOTHING`)
	require.NoError(t, err, "Failed to create test organization")

	return transactor
}

func TestWithTransaction_OuterRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctx := t.Context()
	transactor := setupTransactorTest(t)

	testPoolConfig := sqlc.CreatePoolParams{
		OrgID:     1,
		CreatedAt: time.Now(),
		Url:       "stratum+tcp://test.pool.com:3333",
		Username:  "test.worker",
	}

	// Act
	var poolID int64
	err := transactor.RunInTx(ctx, func(ctx context.Context) error {
		innerErr := transactor.RunInTx(ctx, func(ctx context.Context) error {
			var createErr error
			poolID, createErr = transactor.GetQueries(ctx).CreatePool(ctx, testPoolConfig)
			return createErr
		})
		require.NoError(t, innerErr)

		return sql.ErrTxDone
	})

	// Assert
	require.Error(t, err)

	_, err = transactor.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{OrgID: 1, ID: poolID})
	require.Error(t, err, "Pool should not exist after transaction rollback")
}

func TestWithTransaction_InnerRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctx := t.Context()
	transactor := setupTransactorTest(t)

	testPoolConfig := sqlc.CreatePoolParams{
		OrgID:     1,
		CreatedAt: time.Now(),
		Url:       "stratum+tcp://test.pool.com:3333",
		Username:  "test.worker",
	}

	// Act
	var poolID int64
	err := transactor.RunInTx(ctx, func(ctx context.Context) error {
		var createErr error
		poolID, createErr = transactor.GetQueries(ctx).CreatePool(ctx, testPoolConfig)
		require.NoError(t, createErr)
		return transactor.RunInTx(ctx, func(_ context.Context) error {
			return sql.ErrTxDone
		})
	})

	// Assert
	require.Error(t, err)

	_, err = transactor.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{OrgID: 1, ID: poolID})
	require.Error(t, err, "Pool should not exist after transaction rollback")
}

func TestNestedTransactions_DatabaseLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctx := t.Context()
	transactor := setupTransactorTest(t)

	outerPoolConfig := sqlc.CreatePoolParams{
		OrgID:     1,
		CreatedAt: time.Now(),
		Url:       "stratum+tcp://outer.pool.com:3333",
		Username:  "outer.worker",
	}

	innerPoolConfig := sqlc.CreatePoolParams{
		OrgID:     1,
		CreatedAt: time.Now(),
		Url:       "stratum+tcp://inner.pool.com:3333",
		Username:  "inner.worker",
	}

	// Act - This test will deadlock if nested transactions are created at DB level
	var outerPoolID, innerPoolID int64
	err := transactor.RunInTx(ctx, func(outerCtx context.Context) error {
		var outerErr error
		outerPoolID, outerErr = transactor.GetQueries(outerCtx).CreatePool(outerCtx, outerPoolConfig)
		require.NoError(t, outerErr)

		// Attempt a nested transaction
		return transactor.RunInTx(outerCtx, func(innerCtx context.Context) error {
			var innerErr error
			innerPoolID, innerErr = transactor.GetQueries(innerCtx).CreatePool(innerCtx, innerPoolConfig)
			require.NoError(t, innerErr)

			// If these are truly separate transactions at DB level, this would likely cause a deadlock
			// because the inner transaction would be waiting for a lock held by the outer transaction

			// Read the outer pool from the inner transaction context
			// This should succeed if there's proper transaction handling (same transaction)
			_, getErr := transactor.GetQueries(innerCtx).GetPool(innerCtx, sqlc.GetPoolParams{OrgID: 1, ID: outerPoolID})
			return getErr
		})
	})

	// Assert
	require.NoError(t, err, "Nested transaction operations should complete without deadlock")

	// Verify both pools were created in the same transaction
	// by checking if they both exist
	_, outerErr := transactor.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{OrgID: 1, ID: outerPoolID})
	require.NoError(t, outerErr)

	_, innerErr := transactor.GetQueries(ctx).GetPool(ctx, sqlc.GetPoolParams{OrgID: 1, ID: innerPoolID})
	require.NoError(t, innerErr)
}
