package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// TransactionOutcomeUnknownError means Commit returned an error after the
// server may already have made the transaction durable. Callers must reconcile
// persisted state instead of retrying the transaction under a new key.
type TransactionOutcomeUnknownError struct {
	Err error
}

func (e *TransactionOutcomeUnknownError) Error() string {
	return "transaction commit outcome is unknown: " + e.Err.Error()
}

func (e *TransactionOutcomeUnknownError) Unwrap() error {
	return e.Err
}

// WithTransaction runs action in a transaction and retries the entire action for
// retryable PostgreSQL errors. The action must be safe to replay and should not
// perform side effects outside the transaction.
func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(q sqlc.Querier) (T, error), opts ...*sql.TxOptions) (T, error) {
	return withTransactionWithRetry(ctx, db, action, DefaultRetryConfig, firstTxOpts(opts))
}

func withTransactionWithRetry[T any](ctx context.Context, db *sql.DB, action func(q sqlc.Querier) (T, error), config RetryConfig, txOpts *sql.TxOptions) (T, error) {
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
		resetPoolOnFailover(err, resetPool)
		var outcomeUnknown *TransactionOutcomeUnknownError
		if errors.As(err, &outcomeUnknown) {
			break
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
	var outcomeUnknown *TransactionOutcomeUnknownError
	if errors.As(lastErr, &outcomeUnknown) {
		return zero, lastErr
	}
	var fleetErr fleeterror.FleetError
	if !IsRetryablePostgresError(lastErr) && errors.As(lastErr, &fleetErr) {
		return zero, fleetErr
	}
	return zero, fleeterror.NewInternalErrorf("transaction failed after %d attempts: %w", attempts, lastErr)
}

func executeTransaction[T any](ctx context.Context, db *sql.DB, action func(q sqlc.Querier) (T, error), txOpts *sql.TxOptions) (T, error) {
	var zero T

	//nolint:forbidigo // This helper is the canonical boundary for opening SQL transactions.
	tx, err := db.BeginTx(ctx, txOpts)
	if err != nil {
		return zero, fleeterror.NewInternalErrorf("error opening tx: %w", err)
	}

	//goland:noinspection GoUnhandledErrorResult
	defer tx.Rollback()

	sq := NewTracingQuerier(sqlc.New(tx))
	result, err := action(sq)
	if err != nil {
		return zero, err
	}

	err = tx.Commit()
	if err != nil {
		return zero, &TransactionOutcomeUnknownError{Err: err}
	}

	return result, nil
}

// WithTransactionNoResult runs action in a transaction and retries the entire
// action for retryable PostgreSQL errors. The action must be safe to replay and
// should not perform side effects outside the transaction.
func WithTransactionNoResult(ctx context.Context, db *sql.DB, action func(q sqlc.Querier) error, opts ...*sql.TxOptions) error {
	return withTransactionNoResultWithRetry(ctx, db, action, DefaultRetryConfig, firstTxOpts(opts))
}

// WithTransactionNoRetryNoResult runs action exactly once. Use it only when
// action performs an external side effect that cannot safely be replayed.
func WithTransactionNoRetryNoResult(ctx context.Context, db *sql.DB, action func(q sqlc.Querier) error, opts ...*sql.TxOptions) error {
	_, err := executeTransaction(ctx, db, func(q sqlc.Querier) (struct{}, error) {
		return struct{}{}, action(q)
	}, firstTxOpts(opts))
	return err
}

// WithTransactionTimeout bounds the complete operation, including retries, and
// applies the same limit inside each transaction.
func WithTransactionTimeout[T any](ctx context.Context, db *sql.DB, timeout time.Duration, action func(q sqlc.Querier) (T, error)) (T, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return WithTransaction(timeoutCtx, db, func(q sqlc.Querier) (T, error) {
		var zero T
		if err := q.SetLocalTransactionTimeout(
			timeoutCtx,
			timeout.Milliseconds(),
		); err != nil {
			return zero, err
		}
		return action(q)
	})
}

func WithTransactionTimeoutNoResult(ctx context.Context, db *sql.DB, timeout time.Duration, action func(q sqlc.Querier) error) error {
	_, err := WithTransactionTimeout(ctx, db, timeout, func(q sqlc.Querier) (struct{}, error) {
		return struct{}{}, action(q)
	})
	return err
}

func withTransactionNoResultWithRetry(ctx context.Context, db *sql.DB, action func(q sqlc.Querier) error, config RetryConfig, txOpts *sql.TxOptions) error {
	_, err := withTransactionWithRetry(ctx, db, func(sq sqlc.Querier) (any, error) {
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
