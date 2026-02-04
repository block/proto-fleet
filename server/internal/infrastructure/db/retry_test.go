package db

import (
	"errors"
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
