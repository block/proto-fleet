package db

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// txContextKey carries the tx-bound sqlc.Querier on a context so callers
// that already hold a transaction reuse it instead of opening a new one
// against the base connection.
type txContextKey struct{}

// WithTxQueries returns a child ctx that carries the given tx-bound queries.
// Callers inside a transaction wrap their inner ctx with this so downstream
// store calls and resolver lookups share the same snapshot.
func WithTxQueries(ctx context.Context, q sqlc.Querier) context.Context {
	return context.WithValue(ctx, txContextKey{}, q)
}

// GetTxQueries returns the tx-bound queries on ctx, or nil if the caller
// is not currently inside a transaction.
func GetTxQueries(ctx context.Context) sqlc.Querier {
	if q, ok := ctx.Value(txContextKey{}).(sqlc.Querier); ok {
		return q
	}
	return nil
}
