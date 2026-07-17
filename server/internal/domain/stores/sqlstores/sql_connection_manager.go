package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type SQLConnectionManager struct {
	conn *db.RetryDB
}

func NewSQLConnectionManager(conn *sql.DB) SQLConnectionManager {
	return SQLConnectionManager{conn: db.NewRetryDB(conn)}
}

// GetQueries returns the tx-bound queries when ctx carries them
// (set by SQLTransactor.RunInTx via db.WithTxQueries), otherwise a
// fresh reset-aware handle over the base connection.
func (b *SQLConnectionManager) GetQueries(ctx context.Context) sqlc.Querier {
	if q := db.GetTxQueries(ctx); q != nil {
		return q
	}
	return db.NewFailoverResettingQuerier(b.conn)
}

func (b *SQLConnectionManager) GetTxQueries(ctx context.Context) *sqlc.Queries {
	return db.GetTxQueries(ctx)
}
