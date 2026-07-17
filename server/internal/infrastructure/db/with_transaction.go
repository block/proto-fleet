package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// WithTransaction runs action in a transaction and retries the entire action for
// retryable PostgreSQL errors. The action must be safe to replay and should not
// perform side effects outside the transaction.
func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), opts ...*sql.TxOptions) (T, error) {
	return withTransactionWithRetry(ctx, db, action, DefaultRetryConfig, firstTxOpts(opts))
}

func withTransactionWithRetry[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), config RetryConfig, txOpts *sql.TxOptions) (T, error) {
	var zero T
	var lastErr error
	attempts := 0
	currentBackoff := config.InitialBackoff
	resetPool := poolResetFor(db)

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return zero, fleeterror.NewInternalErrorf("context aborted: %w", ctx.Err())
		default:
		}

		attempts = attempt
		result, err := executeTransaction(ctx, db, action, txOpts)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if resetPool != nil && IsFailoverPostgresError(err) {
			resetPool()
		}
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
			return zero, fleeterror.NewInternalErrorf("context aborted: %w", ctx.Err())
		case <-time.After(sleepDuration):
		}

		currentBackoff = time.Duration(float64(currentBackoff) * config.BackoffMultiplier)
	}

	// Preserve non-retryable FleetError values so business error codes survive
	// the transaction boundary unchanged.
	var fleetErr fleeterror.FleetError
	if !IsRetryablePostgresError(lastErr) && errors.As(lastErr, &fleetErr) {
		return zero, fleetErr
	}
	return zero, fleeterror.NewInternalErrorf("transaction failed after %d attempts: %w", attempts, lastErr)
}

func executeTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), txOpts *sql.TxOptions) (T, error) {
	var zero T

	//nolint:forbidigo // This helper is the canonical boundary for opening SQL transactions.
	tx, err := db.BeginTx(ctx, txOpts)
	if err != nil {
		return zero, fleeterror.NewInternalErrorf("error opening tx: %w", err)
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
		return zero, fleeterror.NewInternalErrorf("error committing tx: %w", err)
	}

	return result, nil
}

// WithTransactionNoResult runs action in a transaction and retries the entire
// action for retryable PostgreSQL errors. The action must be safe to replay and
// should not perform side effects outside the transaction.
func WithTransactionNoResult(ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) error, opts ...*sql.TxOptions) error {
	return withTransactionNoResultWithRetry(ctx, db, action, DefaultRetryConfig, firstTxOpts(opts))
}

func withTransactionNoResultWithRetry(ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) error, config RetryConfig, txOpts *sql.TxOptions) error {
	_, err := withTransactionWithRetry(ctx, db, func(sq *sqlc.Queries) (any, error) {
		var emptyResult any
		return emptyResult, action(sq)
	}, config, txOpts)

	return err
}

// firstTxOpts returns the first element of opts or nil. Used to give
// WithTransaction / WithTransactionNoResult a Go-idiomatic optional
// last-arg signature. Nil preserves the historical default (READ COMMITTED).
func firstTxOpts(opts []*sql.TxOptions) *sql.TxOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return nil
}
