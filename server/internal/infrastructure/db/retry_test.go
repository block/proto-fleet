package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
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
			name:     "admin shutdown",
			err:      &pgconn.PgError{Code: PGAdminShutdown, Message: "terminating connection due to administrator command"},
			expected: true,
		},
		{
			name:     "cannot connect now",
			err:      &pgconn.PgError{Code: PGCannotConnectNow, Message: "the database system is starting up"},
			expected: true,
		},
		{
			name:     "connection failure class",
			err:      &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"},
			expected: true,
		},
		{
			name:     "read only transaction during role change",
			err:      &pgconn.PgError{Code: PGReadOnlySQLTransaction, Message: "cannot execute INSERT in a read-only transaction"},
			expected: true,
		},
		{
			name:     "connection reset string",
			err:      errors.New("read tcp 10.0.0.1:5432: connection reset by peer"),
			expected: true,
		},
		{
			name:     "wrapped EOF string",
			err:      errors.Join(errors.New("query failed"), errors.New("failed to receive message: unexpected EOF")),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFailoverPostgresError(tt.err)
			if result != tt.expected {
				t.Errorf("IsFailoverPostgresError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestRetryOperationRetriesFailoverReadsAndResetsPool(t *testing.T) {
	attempts := 0
	resets := 0

	got, err := retryOperation(context.Background(), "QueryContext", retryOperationOptions{
		retryFailover: true,
		resetPool: func() {
			resets++
		},
	}, func() (int, error) {
		attempts++
		if attempts == 1 {
			return 0, &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
		}
		return 42, nil
	})

	if err != nil {
		t.Fatalf("expected failover-class read retry to succeed: %v", err)
	}
	if got != 42 {
		t.Fatalf("expected result from retry, got %d", got)
	}
	if attempts != 2 {
		t.Fatalf("expected one retry, got %d attempts", attempts)
	}
	if resets != 1 {
		t.Fatalf("expected one pool reset, got %d", resets)
	}
}

func TestRetryOperationResetsPoolForEachFailoverReadFailure(t *testing.T) {
	attempts := 0
	resets := 0

	_, err := retryOperation(context.Background(), "QueryContext", retryOperationOptions{
		retryFailover: true,
		resetPool: func() {
			resets++
		},
	}, func() (int, error) {
		attempts++
		return 0, &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	})

	if err == nil {
		t.Fatal("expected failover-class read operation to fail after max attempts")
	}
	if attempts != DefaultRetryConfig.MaxAttempts {
		t.Fatalf("expected max attempts, got %d attempts", attempts)
	}
	if resets != DefaultRetryConfig.MaxAttempts {
		t.Fatalf("expected pool reset for each failover-class failure, got %d", resets)
	}
}

func TestRetryOperationDoesNotRetryFailoverWrites(t *testing.T) {
	attempts := 0
	resets := 0

	_, err := retryOperation(context.Background(), "ExecContext", retryOperationOptions{
		resetPool: func() {
			resets++
		},
	}, func() (int, error) {
		attempts++
		return 0, &pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"}
	})

	if err == nil {
		t.Fatal("expected failover-class write operation to return the original error")
	}
	if attempts != 1 {
		t.Fatalf("expected no automatic write retry, got %d attempts", attempts)
	}
	if resets != 1 {
		t.Fatalf("expected pool reset before returning write error, got %d", resets)
	}
}

func TestIsReadOnlyQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "select",
			query:    "SELECT 1",
			expected: true,
		},
		{
			name:     "sqlc comment before select",
			query:    "-- name: ListDevices :many\nSELECT * FROM device",
			expected: true,
		},
		{
			name:     "block comment before show",
			query:    "/* health check */ SHOW transaction_read_only",
			expected: true,
		},
		{
			name:     "insert returning is not read-only",
			query:    "INSERT INTO device_set_membership(device_id) VALUES (1) RETURNING device_id",
			expected: false,
		},
		{
			name:     "sqlc comment before insert returning",
			query:    "-- name: AddDevicesToDeviceSet :many\nINSERT INTO device_set_membership(device_id) VALUES (1) RETURNING device_id",
			expected: false,
		},
		{
			name:     "with query is not assumed read-only",
			query:    "WITH deleted AS (DELETE FROM device RETURNING id) SELECT id FROM deleted",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReadOnlyQuery(tt.query); got != tt.expected {
				t.Fatalf("IsReadOnlyQuery(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestRetryDBQueryContextRetriesFailoverAndResetsPool(t *testing.T) {
	state := &retryTestSQLState{
		queryErrs: []error{
			&pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"},
		},
	}
	sqlDB := sql.OpenDB(retryTestConnector{state: state})
	t.Cleanup(func() {
		requireNoError(t, sqlDB.Close())
	})

	resets := 0
	retryDB := NewRetryDB(sqlDB, WithPoolReset(func() {
		resets++
	}))

	rows, err := retryDB.QueryContext(context.Background(), "-- name: ReadHealth :many\nSELECT 1")
	if err != nil {
		t.Fatalf("expected failover-class query retry to succeed: %v", err)
	}
	defer func() {
		requireNoError(t, rows.Close())
	}()
	requireNoError(t, rows.Err())

	if state.queryAttempts() != 2 {
		t.Fatalf("expected one query retry, got %d attempts", state.queryAttempts())
	}
	if resets != 1 {
		t.Fatalf("expected one pool reset, got %d", resets)
	}
}

func TestRetryDBQueryContextDoesNotRetryFailoverForWriteReturning(t *testing.T) {
	state := &retryTestSQLState{
		queryErrs: []error{
			&pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"},
		},
	}
	sqlDB := sql.OpenDB(retryTestConnector{state: state})
	t.Cleanup(func() {
		requireNoError(t, sqlDB.Close())
	})

	resets := 0
	retryDB := NewRetryDB(sqlDB, WithPoolReset(func() {
		resets++
	}))

	rows, err := retryDB.QueryContext(context.Background(), "-- name: AddDevicesToDeviceSet :many\nINSERT INTO device_set_membership(device_id) VALUES (1) RETURNING device_id")
	if rows != nil {
		defer func() {
			requireNoError(t, rows.Close())
			requireNoError(t, rows.Err())
		}()
	}
	if err == nil {
		t.Fatal("expected failover-class write-returning query to return the original error")
	}
	if state.queryAttempts() != 1 {
		t.Fatalf("expected no automatic write-returning retry, got %d attempts", state.queryAttempts())
	}
	if resets != 1 {
		t.Fatalf("expected one pool reset before returning write-returning error, got %d", resets)
	}
}

func TestRetryDBExecContextDoesNotRetryFailoverButResetsPool(t *testing.T) {
	state := &retryTestSQLState{
		execErrs: []error{
			&pgconn.PgError{Code: PGConnectionFailure, Message: "connection failure"},
		},
	}
	sqlDB := sql.OpenDB(retryTestConnector{state: state})
	t.Cleanup(func() {
		requireNoError(t, sqlDB.Close())
	})

	resets := 0
	retryDB := NewRetryDB(sqlDB, WithPoolReset(func() {
		resets++
	}))

	_, err := retryDB.ExecContext(context.Background(), "INSERT 1")
	if err == nil {
		t.Fatal("expected failover-class exec to return the original error")
	}
	if state.execAttempts() != 1 {
		t.Fatalf("expected no automatic exec retry, got %d attempts", state.execAttempts())
	}
	if resets != 1 {
		t.Fatalf("expected one pool reset, got %d", resets)
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

type retryTestSQLState struct {
	mu        sync.Mutex
	queries   int
	execs     int
	queryErrs []error
	execErrs  []error
}

func (s *retryTestSQLState) nextQueryErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++
	if s.queries <= len(s.queryErrs) {
		return s.queryErrs[s.queries-1]
	}
	return nil
}

func (s *retryTestSQLState) nextExecErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execs++
	if s.execs <= len(s.execErrs) {
		return s.execErrs[s.execs-1]
	}
	return nil
}

func (s *retryTestSQLState) queryAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries
}

func (s *retryTestSQLState) execAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execs
}

type retryTestConnector struct {
	state *retryTestSQLState
}

func (c retryTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &retryTestConn{state: c.state}, nil
}

func (c retryTestConnector) Driver() driver.Driver {
	return retryTestDriver{state: c.state}
}

type retryTestDriver struct {
	state *retryTestSQLState
}

func (d retryTestDriver) Open(string) (driver.Conn, error) {
	return &retryTestConn{state: d.state}, nil
}

type retryTestConn struct {
	state *retryTestSQLState
}

func (c *retryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare is not implemented")
}

func (c *retryTestConn) Close() error {
	return nil
}

func (c *retryTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin is not implemented")
}

func (c *retryTestConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if err := c.state.nextQueryErr(); err != nil {
		return nil, err
	}
	return retryTestRows{}, nil
}

func (c *retryTestConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if err := c.state.nextExecErr(); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

type retryTestRows struct{}

func (r retryTestRows) Columns() []string {
	return []string{"value"}
}

func (r retryTestRows) Close() error {
	return nil
}

func (r retryTestRows) Next([]driver.Value) error {
	return io.EOF
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
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
