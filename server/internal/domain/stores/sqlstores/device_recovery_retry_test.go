package sqlstores

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type cloudRecoveryErrorQuerier struct {
	sqlc.Querier
	err error
}

func (q cloudRecoveryErrorQuerier) LockDeviceByIdentifier(context.Context, sqlc.LockDeviceByIdentifierParams) ([]int64, error) {
	return nil, q.err
}

func (q cloudRecoveryErrorQuerier) ReconcileCloudAuthNeededByIdentifier(context.Context, sqlc.ReconcileCloudAuthNeededByIdentifierParams) (sqlc.ReconcileCloudAuthNeededByIdentifierRow, error) {
	return sqlc.ReconcileCloudAuthNeededByIdentifierRow{}, q.err
}

func TestCloudRecoveryPreservesRetryablePostgresError(t *testing.T) {
	for _, stage := range []string{"lock", "reconcile"} {
		for _, code := range []string{db.PGSerializationFailure, db.PGDeadlockDetected} {
			t.Run(stage+"/"+code, func(t *testing.T) {
				retryable := &pgconn.PgError{Code: code}
				ctx := db.WithTxQueries(t.Context(), cloudRecoveryErrorQuerier{err: retryable})
				store := NewSQLDeviceStore(nil)

				var err error
				if stage == "lock" {
					_, err = store.LockDeviceForCloudRecoveryByIdentifier(ctx, "mac:retry", 1)
				} else {
					_, _, err = store.ReconcileCloudAuthenticationNeededPairingStatusByIdentifier(ctx, "mac:retry", 1)
				}

				require.ErrorIs(t, err, retryable)
				require.True(t, db.IsRetryablePostgresError(err))
			})
		}
	}
}
