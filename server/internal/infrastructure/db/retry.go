package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	// MySQL error codes for retryable issues.
	MySQLDeadlockErrCode  = 1213 // Deadlock found when trying to get lock
	MySQLLockWaitErrCode  = 1205 // Lock wait timeout exceeded
	MySQLWSREPNotReady    = 1047 // WSREP has not yet prepared node for application use
	MySQLDuplicateKey     = 1062 // Duplicate key error (retryable in some upsert scenarios)
	defaultMaxRetries     = 3
	defaultRetryBaseDelay = 10 * time.Millisecond
)

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

// DefaultRetryConfig provides sensible default values for retry behavior.
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:       3,
	InitialBackoff:    100 * time.Millisecond,
	MaxBackoff:        2 * time.Second,
	BackoffMultiplier: 2.0,
}

// IsRetryableMySQLError returns true if the error is a MySQL error that may succeed on retry.
// This includes deadlocks, lock wait timeouts, WSREP cluster issues, and duplicate key errors.
func IsRetryableMySQLError(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case MySQLDeadlockErrCode, MySQLLockWaitErrCode, MySQLWSREPNotReady, MySQLDuplicateKey:
			return true
		}
	}
	return false
}

// RetryDB wraps a *sql.DB and automatically retries database operations on retryable errors.
// This provides transparent retry handling for SQL operations without requiring
// explicit retry logic at each call site.
//
// Methods with retry logic: ExecContext, QueryContext.
// Methods without retry: QueryRowContext (error is deferred to Scan), PrepareContext.
type RetryDB struct {
	*sql.DB
}

// NewRetryDB creates a new RetryDB wrapper around the given database connection.
func NewRetryDB(db *sql.DB) *RetryDB {
	return &RetryDB{DB: db}
}

// ExecContext executes a query with automatic retry on retryable MySQL errors.
// It wraps the underlying sql.DB.ExecContext with exponential backoff retry logic.
func (r *RetryDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var lastErr error

	for attempt := 1; attempt <= defaultMaxRetries; attempt++ {
		result, err := r.DB.ExecContext(ctx, query, args...)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !IsRetryableMySQLError(err) {
			return nil, fmt.Errorf("ExecContext: %w", err)
		}

		if attempt == defaultMaxRetries {
			return nil, fmt.Errorf("ExecContext failed after %d attempts: %w", defaultMaxRetries, lastErr)
		}

		delay := defaultRetryBaseDelay << (attempt - 1)
		slog.Warn("retryable MySQL error in ExecContext, retrying",
			"attempt", attempt,
			"max_retries", defaultMaxRetries,
			"delay", delay,
			"error", err)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("ExecContext failed after %d attempts: %w", defaultMaxRetries, lastErr)
}

// QueryContext executes a query with automatic retry on retryable MySQL errors.
// It wraps the underlying sql.DB.QueryContext with exponential backoff retry logic.
func (r *RetryDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	var lastErr error

	for attempt := 1; attempt <= defaultMaxRetries; attempt++ {
		rows, err := r.DB.QueryContext(ctx, query, args...)
		if err == nil {
			return rows, nil
		}

		lastErr = err
		if !IsRetryableMySQLError(err) {
			return nil, fmt.Errorf("QueryContext: %w", err)
		}

		if attempt == defaultMaxRetries {
			return nil, fmt.Errorf("QueryContext failed after %d attempts: %w", defaultMaxRetries, lastErr)
		}

		delay := defaultRetryBaseDelay << (attempt - 1)
		slog.Warn("retryable MySQL error in QueryContext, retrying",
			"attempt", attempt,
			"max_retries", defaultMaxRetries,
			"delay", delay,
			"error", err)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("QueryContext failed after %d attempts: %w", defaultMaxRetries, lastErr)
}
