package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

var _ interfaces.Transactor = &SQLTransactor{}

type SQLTransactor struct {
	SQLConnectionManager
}

func NewSQLTransactor(conn *sql.DB, opts ...ConnectionManagerOption) *SQLTransactor {
	return &SQLTransactor{
		SQLConnectionManager: NewSQLConnectionManager(conn, opts...),
	}
}

func (f *SQLTransactor) RunInTx(ctx context.Context, action func(ctx context.Context) error) error {
	_, err := f.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		var emptyResult any
		return emptyResult, action(ctx)
	})
	return err
}

func (f *SQLTransactor) RunInTxWithResult(ctx context.Context, action func(ctx context.Context) (any, error)) (any, error) {
	if f.GetTxQueries(ctx) != nil {
		// If the context already has a transaction, just use the existing context
		return action(ctx)
	}
	// Pass the underlying *sql.DB to WithTransaction (which has its own retry logic)
	return db.WithTransaction(ctx, f.conn.DB, func(q *sqlc.Queries) (any, error) {
		txCtx := db.WithTxQueries(ctx, q)
		return action(txCtx)
	})
}
