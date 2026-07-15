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

type ConnectionManagerOption = db.RetryDBOption

func NewSQLConnectionManager(conn *sql.DB, opts ...ConnectionManagerOption) SQLConnectionManager {
	return SQLConnectionManager{conn: db.NewRetryDB(conn, opts...)}
}

// GetQueries returns the tx-bound queries when ctx carries them
// (set by SQLTransactor.RunInTx via db.WithTxQueries), otherwise a
// fresh handle over the base connection.
func (b *SQLConnectionManager) GetQueries(ctx context.Context) *sqlc.Queries {
	if q := db.GetTxQueries(ctx); q != nil {
		return q
	}
	return sqlc.New(b.conn)
}

func (b *SQLConnectionManager) GetTxQueries(ctx context.Context) *sqlc.Queries {
	return db.GetTxQueries(ctx)
}
