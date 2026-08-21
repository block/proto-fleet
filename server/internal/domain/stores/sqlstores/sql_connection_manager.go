package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type SQLConnectionManager struct {
	conn    *db.RetryDB
	queries sqlc.Querier
}

func NewSQLConnectionManager(conn *sql.DB) SQLConnectionManager {
	retryDB := db.NewRetryDB(conn)
	return SQLConnectionManager{
		conn:    retryDB,
		queries: db.NewFailoverResettingQuerier(retryDB),
	}
}

// GetQueries returns the tx-bound queries when ctx carries them
// (set by SQLTransactor.RunInTx via db.WithTxQueries), otherwise a
// reset-aware handle over the base connection.
func (b *SQLConnectionManager) GetQueries(ctx context.Context) sqlc.Querier {
	if q := db.GetTxQueries(ctx); q != nil {
		return q
	}
	return b.queries
}

func (b *SQLConnectionManager) GetTxQueries(ctx context.Context) sqlc.Querier {
	return db.GetTxQueries(ctx)
}
