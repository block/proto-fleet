package sqlstores

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type errorBatchFailureQuerier struct {
	sqlc.Querier
	err   error
	calls *int
}

func (q errorBatchFailureQuerier) UpsertDeviceErrors(context.Context, sqlc.UpsertDeviceErrorsParams) (bool, error) {
	*q.calls++
	return false, q.err
}

func TestUpsertErrorsPreservesRetryablePostgresErrorInTransaction(t *testing.T) {
	retryable := &pgconn.PgError{Code: db.PGDeadlockDetected, Message: "deadlock detected"}
	calls := 0
	ctx := db.WithTxQueries(t.Context(), errorBatchFailureQuerier{err: retryable, calls: &calls})
	store := NewSQLErrorStore(nil, nil)

	err := store.UpsertErrors(ctx, 1, "device", nil, time.Now(), time.Minute, false)

	require.ErrorIs(t, err, retryable)
	require.True(t, db.IsRetryablePostgresError(err))
	require.Equal(t, 1, calls, "the outer transaction owns retries")
}
