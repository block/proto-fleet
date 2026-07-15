package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// PostgreSQL SQLSTATE codes for retryable issues.
	// See: https://www.postgresql.org/docs/current/errcodes-appendix.html
	PGSerializationFailure   = "40001" // serialization_failure
	PGDeadlockDetected       = "40P01" // deadlock_detected
	PGReadOnlySQLTransaction = "25006" // read_only_sql_transaction
	PGAdminShutdown          = "57P01" // admin_shutdown
	PGCrashShutdown          = "57P02" // crash_shutdown
	PGCannotConnectNow       = "57P03" // cannot_connect_now
	PGConnectionFailure      = "08006" // connection_failure
	// PGUniqueViolation is NOT retryable at the infrastructure level.
	// Unique violations should be handled at the application level (e.g., "username already exists").
	// Retrying would cause unnecessary delays for errors that will never succeed.
	PGUniqueViolation = "23505" // unique_violation - exported for application-level handling
)

var failoverErrorSubstrings = []string{
	"bad connection",
	"broken pipe",
	"connection refused",
	"connection reset by peer",
	"connection is already closed",
	"server closed the connection unexpectedly",
	"failed to receive message: unexpected eof",
	"unexpected eof",
	"the database system is starting up",
	"the database system is shutting down",
	"the database system is in recovery mode",
}

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

func IsFailoverPostgresError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if strings.HasPrefix(pgErr.Code, "08") {
			return true
		}
		switch pgErr.Code {
		case PGAdminShutdown, PGCrashShutdown, PGCannotConnectNow, PGReadOnlySQLTransaction:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range failoverErrorSubstrings {
		if strings.Contains(msg, needle) {
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
	resetPool func()
}

type RetryDBOption func(*RetryDB)

func WithPoolReset(reset func()) RetryDBOption {
	return func(r *RetryDB) {
		r.resetPool = reset
	}
}

// NewRetryDB creates a new RetryDB wrapper around the given database connection.
func NewRetryDB(db *sql.DB, opts ...RetryDBOption) *RetryDB {
	r := &RetryDB{DB: db}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func NewIdleConnectionPoolReset(conn *sql.DB, maxIdleConns int) func() {
	return func() {
		conn.SetMaxIdleConns(0)
		conn.SetMaxIdleConns(maxIdleConns)
	}
}

func IsReadOnlyQuery(query string) bool {
	keyword := firstSQLKeyword(query)
	switch keyword {
	case "select", "show", "values":
		return true
	default:
		return false
	}
}

func firstSQLKeyword(query string) string {
	remaining := strings.TrimSpace(query)
	for {
		switch {
		case strings.HasPrefix(remaining, "--"):
			lineEnd := strings.IndexByte(remaining, '\n')
			if lineEnd < 0 {
				return ""
			}
			remaining = strings.TrimSpace(remaining[lineEnd+1:])
		case strings.HasPrefix(remaining, "/*"):
			commentEnd := strings.Index(remaining, "*/")
			if commentEnd < 0 {
				return ""
			}
			remaining = strings.TrimSpace(remaining[commentEnd+2:])
		default:
			for i, r := range remaining {
				if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
					return strings.ToLower(remaining[:i])
				}
			}
			return strings.ToLower(remaining)
		}
	}
}

type retryOperationOptions struct {
	retryFailover bool
	resetPool     func()
}

// retryOperation executes fn with retry logic on retryable PostgreSQL errors.
// Uses exponential backoff between retries, respecting context cancellation.
func retryOperation[T any](ctx context.Context, opName string, opts retryOperationOptions, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error
	currentBackoff := DefaultRetryConfig.InitialBackoff

	for attempt := 1; attempt <= DefaultRetryConfig.MaxAttempts; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		retryable := IsRetryablePostgresError(err)
		failoverClass := IsFailoverPostgresError(err)
		if failoverClass && opts.resetPool != nil {
			opts.resetPool()
		}
		if !retryable && !(opts.retryFailover && failoverClass) {
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
			"failover_class", failoverClass,
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
	return retryOperation(ctx, "ExecContext", retryOperationOptions{resetPool: r.resetPool}, func() (sql.Result, error) {
		return r.DB.ExecContext(ctx, query, args...)
	})
}

// QueryContext executes a query with automatic retry on retryable PostgreSQL errors.
func (r *RetryDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return retryOperation(ctx, "QueryContext", retryOperationOptions{retryFailover: IsReadOnlyQuery(query), resetPool: r.resetPool}, func() (*sql.Rows, error) {
		return r.DB.QueryContext(ctx, query, args...)
	})
}
