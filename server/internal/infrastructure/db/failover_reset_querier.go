package db

import (
	"context"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// NewFailoverResettingQuerier returns a normal sqlc handle whose complete
// operations reset stale idle connections when scan/deferred errors indicate a
// PostgreSQL failover. It intentionally does not add another retry layer.
func NewFailoverResettingQuerier(conn *RetryDB) sqlc.Querier {
	return newFailoverResettingQuerier(sqlc.New(conn), conn.resetPool)
}

func newFailoverResettingQuerier(next sqlc.Querier, resetPool func()) sqlc.Querier {
	return sqlc.NewRetryingQuerier(next, failoverResetRetrier{resetPool: resetPool})
}

type failoverResetRetrier struct {
	resetPool func()
}

func (r failoverResetRetrier) RetryQuery(_ context.Context, _ string, fn func() error) error {
	return markAfterPoolReset(fn(), r.resetPool)
}

type failoverResetError struct {
	err error
}

func (e failoverResetError) Error() string {
	return e.err.Error()
}

func (e failoverResetError) Unwrap() error {
	return e.err
}

func markFailoverReset(err error) error {
	if err == nil || wasPoolResetForFailover(err) {
		return err
	}
	return failoverResetError{err: err}
}

func markAfterPoolReset(err error, resetPool func()) error {
	if resetPoolOnFailover(err, resetPool) {
		return markFailoverReset(err)
	}
	return err
}

func resetPoolOnFailover(err error, resetPool func()) bool {
	if resetPool == nil || !IsFailoverPostgresError(err) || wasPoolResetForFailover(err) {
		return false
	}
	resetPool()
	return true
}

func wasPoolResetForFailover(err error) bool {
	var resetErr failoverResetError
	return errors.As(err, &resetErr)
}
