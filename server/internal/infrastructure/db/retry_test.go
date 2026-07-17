package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryablePostgresError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "serialization failure error",
			err:      &pgconn.PgError{Code: PGSerializationFailure, Message: "could not serialize access"},
			expected: true,
		},
		{
			name:     "deadlock detected error",
			err:      &pgconn.PgError{Code: PGDeadlockDetected, Message: "deadlock detected"},
			expected: true,
		},
		{
			name:     "unique violation error - NOT retryable at infra level",
			err:      &pgconn.PgError{Code: PGUniqueViolation, Message: "duplicate key value"},
			expected: false, // Unique violations should be handled at application level
		},
		{
			name:     "other postgres error - syntax",
			err:      &pgconn.PgError{Code: "42601", Message: "syntax error"},
			expected: false,
		},
		{
			name:     "other postgres error - insufficient privilege",
			err:      &pgconn.PgError{Code: "42501", Message: "permission denied"},
			expected: false,
		},
		{
			name:     "wrapped deadlock error",
			err:      errors.Join(errors.New("context"), &pgconn.PgError{Code: PGDeadlockDetected}),
			expected: true,
		},
		{
			name:     "deeply wrapped retryable error",
			err:      errors.Join(errors.New("outer"), errors.Join(errors.New("inner"), &pgconn.PgError{Code: PGSerializationFailure})),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryablePostgresError(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryablePostgresError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsFailoverPostgresError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "connection sqlstate class", err: &pgconn.PgError{Code: PGConnectionFailure}, want: true},
		{name: "read only transaction", err: &pgconn.PgError{Code: PGReadOnlySQLTransaction}, want: true},
		{name: "admin shutdown", err: &pgconn.PgError{Code: PGAdminShutdown}, want: true},
		{name: "wrapped sqlstate", err: fmt.Errorf("query failed: %w", &pgconn.PgError{Code: PGCannotConnectNow}), want: true},
		{name: "driver bad connection sentinel", err: driver.ErrBadConn, want: true},
		{name: "wrapped unexpected eof sentinel", err: fmt.Errorf("driver read failed: %w", io.ErrUnexpectedEOF), want: true},
		{name: "wrapped connection reset errno", err: fmt.Errorf("read tcp: %w", syscall.ECONNRESET), want: true},
		{name: "unrelated postgres error", err: &pgconn.PgError{Code: PGUniqueViolation}, want: false},
		{name: "unrelated postgres error with failover-like message", err: &pgconn.PgError{Code: PGUniqueViolation, Message: "duplicate key value contains bad connection"}, want: false},
		{name: "wrapped unrelated postgres error with failover-like outer message", err: fmt.Errorf("unexpected eof while handling request: %w", &pgconn.PgError{Code: "22P02", Message: "invalid input syntax"}), want: false},
		{name: "plain failover-like text", err: errors.New("driver: bad connection"), want: false},
		{name: "generic error", err: errors.New("syntax error"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsFailoverPostgresError(tt.err); got != tt.want {
				t.Fatalf("IsFailoverPostgresError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRetryConfig(t *testing.T) {
	// Verify DefaultRetryConfig has sensible values
	if DefaultRetryConfig.MaxAttempts < 1 {
		t.Errorf("MaxAttempts should be at least 1, got %d", DefaultRetryConfig.MaxAttempts)
	}
	if DefaultRetryConfig.InitialBackoff <= 0 {
		t.Errorf("InitialBackoff should be positive, got %v", DefaultRetryConfig.InitialBackoff)
	}
	if DefaultRetryConfig.MaxBackoff < DefaultRetryConfig.InitialBackoff {
		t.Errorf("MaxBackoff (%v) should be >= InitialBackoff (%v)",
			DefaultRetryConfig.MaxBackoff, DefaultRetryConfig.InitialBackoff)
	}
	if DefaultRetryConfig.BackoffMultiplier < 1 {
		t.Errorf("BackoffMultiplier should be >= 1, got %v", DefaultRetryConfig.BackoffMultiplier)
	}
}

func TestExponentialBackoffCalculation(t *testing.T) {
	// Verify the exponential backoff formula using DefaultRetryConfig
	// InitialBackoff=100ms, BackoffMultiplier=2.0, MaxBackoff=2s
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 100 * time.Millisecond},  // initial backoff
		{2, 200 * time.Millisecond},  // 100ms * 2.0
		{3, 400 * time.Millisecond},  // 200ms * 2.0
		{4, 800 * time.Millisecond},  // 400ms * 2.0
		{5, 1600 * time.Millisecond}, // 800ms * 2.0
		{6, 2 * time.Second},         // capped at MaxBackoff
	}

	currentBackoff := DefaultRetryConfig.InitialBackoff
	for _, tt := range tests {
		delay := currentBackoff
		if delay > DefaultRetryConfig.MaxBackoff {
			delay = DefaultRetryConfig.MaxBackoff
		}
		if delay != tt.expected {
			t.Errorf("attempt %d: expected delay %v, got %v", tt.attempt, tt.expected, delay)
		}
		currentBackoff = time.Duration(float64(currentBackoff) * DefaultRetryConfig.BackoffMultiplier)
	}
}

func TestRetrierRetryQuery(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		wantCalls  int
		succeedOn  int
		wantErrMsg string
	}{
		{
			name:      "retries serialization failure",
			code:      PGSerializationFailure,
			wantCalls: 2,
			succeedOn: 2,
		},
		{
			name:       "does not retry non-retryable error",
			code:       "42601",
			wantCalls:  1,
			wantErrMsg: "TestQuery",
		},
		{
			name:       "stops after max attempts",
			code:       PGSerializationFailure,
			wantCalls:  DefaultRetryConfig.MaxAttempts,
			wantErrMsg: fmt.Sprintf("failed after %d attempts", DefaultRetryConfig.MaxAttempts),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retrier := Retrier{}
			calls := 0
			pgErr := &pgconn.PgError{Code: tt.code, Message: tt.name}
			err := retrier.RetryQuery(context.Background(), "TestQuery", func() error {
				calls++
				if tt.succeedOn != 0 && calls == tt.succeedOn {
					return nil
				}
				return pgErr
			})

			if calls != tt.wantCalls {
				t.Fatalf("callback calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.succeedOn != 0 {
				if err != nil {
					t.Fatalf("RetryQuery error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, pgErr) {
				t.Fatalf("RetryQuery error = %v, want wrapped PostgreSQL error", err)
			}
			if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Fatalf("RetryQuery error = %q, want substring %q", err, tt.wantErrMsg)
			}
		})
	}
}

func TestRetrierHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := (Retrier{}).RetryQuery(ctx, "CancelledQuery", func() error {
		calls++
		return &pgconn.PgError{Code: PGDeadlockDetected}
	})

	if calls != 1 {
		t.Fatalf("callback calls = %d, want 1", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RetryQuery error = %v, want context.Canceled", err)
	}
}

func TestRetrierResetsPoolOnFailoverWithoutRetry(t *testing.T) {
	resets := 0
	calls := 0
	pgErr := &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}

	err := (Retrier{resetPool: func() {
		resets++
	}}).RetryQuery(context.Background(), "FailoverQuery", func() error {
		calls++
		return pgErr
	})

	if !errors.Is(err, pgErr) {
		t.Fatalf("RetryQuery error = %v, want wrapped failover error", err)
	}
	if calls != 1 {
		t.Fatalf("callback calls = %d, want 1", calls)
	}
	if resets != 1 {
		t.Fatalf("pool resets = %d, want 1", resets)
	}
}
