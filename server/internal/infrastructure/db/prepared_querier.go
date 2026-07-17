package db

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// PreparedQuerier owns a prepared sqlc handle whose complete query methods are retried.
type PreparedQuerier struct {
	sqlc.Querier
	prepared *sqlc.Queries
}

// NewPreparedQuerier prepares every sqlc query and wraps the resulting handle with retries.
func NewPreparedQuerier(ctx context.Context, conn *sql.DB) (*PreparedQuerier, error) {
	prepared, err := sqlc.Prepare(ctx, conn)
	if err != nil {
		return nil, err
	}
	return &PreparedQuerier{
		Querier:  sqlc.NewRetryingQuerier(prepared, NewRetrier(conn)),
		prepared: prepared,
	}, nil
}

// Close releases the prepared statements owned by this handle.
func (p *PreparedQuerier) Close() error {
	return p.prepared.Close()
}
