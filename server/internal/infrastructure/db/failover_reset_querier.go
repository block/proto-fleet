package db

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// NewFailoverResettingQuerier returns a normal sqlc handle whose complete
// operations reset stale idle connections when scan/deferred errors indicate a
// PostgreSQL failover. It intentionally does not add another retry layer.
func NewFailoverResettingQuerier(conn *RetryDB) sqlc.Querier {
	return sqlc.NewRetryingQuerier(sqlc.New(conn), failoverResetRetrier{conn: conn})
}

type failoverResetRetrier struct {
	conn *RetryDB
}

func (r failoverResetRetrier) RetryQuery(_ context.Context, _ string, fn func() error) error {
	err := fn()
	r.conn.ResetPoolOnFailover(err)
	return err
}
