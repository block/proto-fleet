package db

import (
	"errors"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestIsRetryableMySQLError(t *testing.T) {
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
			name:     "deadlock error",
			err:      &mysql.MySQLError{Number: MySQLDeadlockErrCode, Message: "Deadlock found"},
			expected: true,
		},
		{
			name:     "lock wait timeout error",
			err:      &mysql.MySQLError{Number: MySQLLockWaitErrCode, Message: "Lock wait timeout"},
			expected: true,
		},
		{
			name:     "WSREP not ready error",
			err:      &mysql.MySQLError{Number: MySQLWSREPNotReady, Message: "WSREP not ready"},
			expected: true,
		},
		{
			name:     "duplicate key error",
			err:      &mysql.MySQLError{Number: MySQLDuplicateKey, Message: "Duplicate entry"},
			expected: true,
		},
		{
			name:     "other mysql error - syntax",
			err:      &mysql.MySQLError{Number: 1064, Message: "Syntax error"},
			expected: false,
		},
		{
			name:     "other mysql error - access denied",
			err:      &mysql.MySQLError{Number: 1045, Message: "Access denied"},
			expected: false,
		},
		{
			name:     "wrapped deadlock error",
			err:      errors.Join(errors.New("context"), &mysql.MySQLError{Number: MySQLDeadlockErrCode}),
			expected: true,
		},
		{
			name:     "deeply wrapped retryable error",
			err:      errors.Join(errors.New("outer"), errors.Join(errors.New("inner"), &mysql.MySQLError{Number: MySQLLockWaitErrCode})),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableMySQLError(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryableMySQLError(%v) = %v, want %v", tt.err, result, tt.expected)
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
	// Verify the exponential backoff formula produces expected delays
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 10 * time.Millisecond},  // 10ms << 0 = 10ms
		{2, 20 * time.Millisecond},  // 10ms << 1 = 20ms
		{3, 40 * time.Millisecond},  // 10ms << 2 = 40ms
		{4, 80 * time.Millisecond},  // 10ms << 3 = 80ms
		{5, 160 * time.Millisecond}, // 10ms << 4 = 160ms
	}

	for _, tt := range tests {
		delay := defaultRetryBaseDelay << (tt.attempt - 1)
		if delay != tt.expected {
			t.Errorf("attempt %d: expected delay %v, got %v", tt.attempt, tt.expected, delay)
		}
	}
}
