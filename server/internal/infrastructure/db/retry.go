package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// PostgreSQL SQLSTATE codes for retryable issues.
	// See: https://www.postgresql.org/docs/current/errcodes-appendix.html
	PGSerializationFailure = "40001" // serialization_failure
	PGDeadlockDetected     = "40P01" // deadlock_detected
	// PGUniqueViolation is NOT retryable at the infrastructure level.
	// Unique violations should be handled at the application level (e.g., "username already exists").
	// Retrying would cause unnecessary delays for errors that will never succeed.
	PGUniqueViolation = "23505" // unique_violation - exported for application-level handling
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

// IsRetryablePostgresError returns true if the error is a PostgreSQL error that may succeed on retry.
// This includes serialization failures and deadlocks. Unique violations are NOT retryable at this level
// since they indicate a constraint violation that won't succeed on retry.
func IsRetryablePostgresError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case PGSerializationFailure, PGDeadlockDetected:
			return true
		}
	}
	return false
}

// IsUniqueViolationError returns true if the error is a PostgreSQL unique constraint violation.
func IsUniqueViolationError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == PGUniqueViolation
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

// retryOperation executes fn with retry logic on retryable PostgreSQL errors.
// Uses exponential backoff between retries, respecting context cancellation.
func retryOperation[T any](ctx context.Context, opName string, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error
	currentBackoff := DefaultRetryConfig.InitialBackoff

	for attempt := 1; attempt <= DefaultRetryConfig.MaxAttempts; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !IsRetryablePostgresError(err) {
			return zero, fmt.Errorf("%s: %w", opName, err)
		}

		if attempt == DefaultRetryConfig.MaxAttempts {
			return zero, fmt.Errorf("%s failed after %d attempts: %w", opName, DefaultRetryConfig.MaxAttempts, lastErr)
		}

		delay := currentBackoff
		if delay > DefaultRetryConfig.MaxBackoff {
			delay = DefaultRetryConfig.MaxBackoff
		}
		slog.Warn("retryable PostgreSQL error, retrying",
			"operation", opName,
			"attempt", attempt,
			"max_retries", DefaultRetryConfig.MaxAttempts,
			"delay", delay,
			"error", err)

		select {
		case <-ctx.Done():
			return zero, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
		}

		currentBackoff = time.Duration(float64(currentBackoff) * DefaultRetryConfig.BackoffMultiplier)
	}

	return zero, fmt.Errorf("%s failed after %d attempts: %w", opName, DefaultRetryConfig.MaxAttempts, lastErr)
}

// ExecContext executes a query with automatic retry on retryable PostgreSQL errors.
func (r *RetryDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return retryOperation(ctx, "ExecContext", func() (sql.Result, error) {
		return r.DB.ExecContext(ctx, query, args...)
	})
}

// QueryContext executes a query with automatic retry on retryable PostgreSQL errors.
func (r *RetryDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return retryOperation(ctx, "QueryContext", func() (*sql.Rows, error) {
		return r.DB.QueryContext(ctx, query, args...)
	})
}
