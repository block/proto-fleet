package sqlstores

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// stubQuerier embeds sqlc.Querier so only the methods a test exercises need
// implementing; any other call nil-panics, which never happens here.
type stubQuerier struct {
	sqlc.Querier
	lockErr error
}

func (s stubQuerier) LockRackPlacementForWrite(context.Context, sqlc.LockRackPlacementForWriteParams) (sqlc.LockRackPlacementForWriteRow, error) {
	return sqlc.LockRackPlacementForWriteRow{}, s.lockErr
}

// TestLockRackPlacementForWrite_PreservesRetryablePgError pins the transaction
// retry contract for the lock the slot-write path acquires: a serialization
// failure raised by the lock query must stay reachable through the store's
// error wrapping (%w, not %v) so db.IsRetryablePostgresError — and therefore
// WithTransaction — still fires. A %v wrap flattens the *pgconn.PgError out of
// the chain and the tx would surface a dead 500 instead of retrying.
func TestLockRackPlacementForWrite_PreservesRetryablePgError(t *testing.T) {
	store := NewSQLCollectionStore(nil)
	// GetQueries returns the ctx-bound querier when present, so the stub stands
	// in for the tx queries without a real DB connection.
	ctx := db.WithTxQueries(context.Background(), stubQuerier{
		lockErr: &pgconn.PgError{Code: db.PGSerializationFailure, Message: "could not serialize access"},
	})

	_, err := store.LockRackPlacementForWrite(ctx, 1, 1)

	require.Error(t, err)
	assert.True(t, db.IsRetryablePostgresError(err),
		"serialization failure must survive the store's error wrapping so the tx can retry")
}
