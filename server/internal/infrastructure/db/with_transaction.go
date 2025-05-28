package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

// DefaultRetryConfig provides sensible default values for retry behavior
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:       3,
	InitialBackoff:    100 * time.Millisecond,
	MaxBackoff:        2 * time.Second,
	BackoffMultiplier: 2.0,
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific error types
	var sqlErr *mysql.MySQLError // were using the mysql driver so explicitly checking those here
	if errors.As(err, &sqlErr) {
		// Add specific error codes that should be retried
		switch sqlErr.Number {
		case 1213: // Deadlock found when trying to get lock
		case 1205: // Lock wait timeout exceeded
		case 1047: // WSREP has not yet prepared node for application use
			return true
		}
	}

	return false
}

func WithTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error)) (T, error) {
	return WithTransactionWithRetry(ctx, db, action, DefaultRetryConfig)
}

func WithTransactionWithRetry[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error), config RetryConfig) (T, error) {
	var zero T
	var lastErr error
	currentBackoff := config.InitialBackoff

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return zero, fleeterror.NewInternalErrorf("context aborted: %v", ctx.Err())
		default:
		}

		result, err := executeTransaction(ctx, db, action)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !isRetryableError(err) || attempt == config.MaxAttempts {
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

	return zero, fleeterror.NewInternalErrorf("transaction failed after %d attempts: %v", config.MaxAttempts, lastErr)
}

func executeTransaction[T any](ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) (T, error)) (T, error) {
	var zero T

	tx, err := db.BeginTx(ctx, nil)
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
	return WithTransactionNoResultWithRetry(ctx, db, action, DefaultRetryConfig)
}

func WithTransactionNoResultWithRetry(ctx context.Context, db *sql.DB, action func(q *sqlc.Queries) error, config RetryConfig) error {
	_, err := WithTransactionWithRetry(ctx, db, func(sq *sqlc.Queries) (any, error) {
		var emptyResult any
		return emptyResult, action(sq)
	}, config)

	return err
}
