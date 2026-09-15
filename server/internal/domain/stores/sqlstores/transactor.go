package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

var _ interfaces.Transactor = &SQLTransactor{}

type SQLTransactor struct {
	SQLConnectionManager
}

func NewSQLTransactor(conn *sql.DB) *SQLTransactor {
	return &SQLTransactor{
		SQLConnectionManager: NewSQLConnectionManager(conn),
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
	return db.WithTransaction(ctx, f.conn.DB, func(q sqlc.Querier) (any, error) {
		txCtx := db.WithTxQueries(ctx, q)
		return action(txCtx)
	})
}

// RunInTxNoRetry holds transaction-bound locks while action performs side
// effects outside this transaction. Reject nesting because the outer
// transaction could retry and replay those effects.
func (f *SQLTransactor) RunInTxNoRetry(ctx context.Context, action func(context.Context) error) error {
	if f.GetTxQueries(ctx) != nil {
		return fleeterror.NewInternalError("non-retryable transaction cannot be nested")
	}
	return db.WithTransactionNoRetryNoResult(ctx, f.conn.DB, func(q sqlc.Querier) error {
		return action(db.WithTxQueries(ctx, q))
	})
}
