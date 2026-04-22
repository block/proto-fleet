package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error)) (T, error) {
	return withTransactionWithRetry(ctx, db, nil, action, DefaultRetryConfig)
}

// WithReadOnlyTransaction runs action inside a read-only REPEATABLE READ
// transaction so every query in action sees a single consistent snapshot of
// the database. Use this for multi-query read paths whose correctness depends
// on the queries agreeing with each other (e.g. a header + aggregate counts
// + per-row list triple where a concurrent delete between queries would
// produce mismatched totals vs. row counts).
//
// Retry semantics, FleetError preservation, and context cancellation behavior
// are identical to WithTransaction.
func WithReadOnlyTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error)) (T, error) {
	return withTransactionWithRetry(ctx, db, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	}, action, DefaultRetryConfig)
}

func withTransactionWithRetry[T any](ctx context.Context, db *sql.DB, opts *sql.TxOptions, action func(q *sqlc.Queries) (T, error), config RetryConfig) (T, error) {
	var zero T
	var lastErr error
	currentBackoff := config.InitialBackoff

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return zero, fleeterror.NewInternalErrorf("context aborted: %v", ctx.Err())
		default:
		}

		result, err := executeTransaction(ctx, db, opts, action)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !IsRetryablePostgresError(err) || attempt == config.MaxAttempts {
			break
		}

		// Calculate next backoff duration
		sleepDuration := currentBackoff
		if sleepDuration > config.MaxBackoff {
			sleepDuration = config.MaxBackoff
		}

		select {
		case <-ctx.Done():
			return zero, fleeterror.NewInternalErrorf("context aborted: %v", ctx.Err())
		case <-time.After(sleepDuration):
		}

		currentBackoff = time.Duration(float64(currentBackoff) * config.BackoffMultiplier)
	}

	// Preserve FleetError if the original error was already a business error
	var fleetErr fleeterror.FleetError
	if errors.As(lastErr, &fleetErr) {
		return zero, fleetErr
	}
	return zero, fleeterror.NewInternalErrorf("transaction failed after %d attempts: %v", config.MaxAttempts, lastErr)
}

func executeTransaction[T any](ctx context.Context, db *sql.DB, opts *sql.TxOptions, action func(q *sqlc.Queries) (T, error)) (T, error) {
	var zero T

	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return zero, fleeterror.NewInternalErrorf("error opening tx: %v", err)
	}

	//goland:noinspection GoUnhandledErrorResult
	defer tx.Rollback()

	sq := sqlc.New(tx)
	result, err := action(sq)
	if err != nil {
		return zero, err
	}

	err = tx.Commit()
	if err != nil {
		return zero, fleeterror.NewInternalErrorf("error committing tx: %v", err)
	}

	return result, nil
}

func WithTransactionNoResult(ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) error) error {
	return withTransactionNoResultWithRetry(ctx, db, action, DefaultRetryConfig)
}

func withTransactionNoResultWithRetry(ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) error, config RetryConfig) error {
	_, err := withTransactionWithRetry(ctx, db, nil, func(sq *sqlc.Queries) (any, error) {
		var emptyResult any
		return emptyResult, action(sq)
	}, config)

	return err
}
