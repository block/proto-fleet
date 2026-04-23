package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), opts ...*sql.TxOptions) (T, error) {
	return withTransactionWithRetry(ctx, db, action, DefaultRetryConfig, firstTxOpts(opts))
}

func withTransactionWithRetry[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), config RetryConfig, txOpts *sql.TxOptions) (T, error) {
	var zero T
	var lastErr error
	currentBackoff := config.InitialBackoff

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return zero, fleeterror.NewInternalErrorf("context aborted: %v", ctx.Err())
		default:
		}

		result, err := executeTransaction(ctx, db, action, txOpts)
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

func executeTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), txOpts *sql.TxOptions) (T, error) {
	var zero T

	tx, err := db.BeginTx(ctx, txOpts)
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
