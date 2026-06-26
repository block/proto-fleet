package testutil

import (
	"errors"
	"testing"
)

func TestIsRetryableMigrationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "deadlock sqlstate",
			err:  errors.New("migration failed: ERROR: deadlock detected (SQLSTATE 40P01)"),
			want: true,
		},
		{
			name: "serialization sqlstate",
			err:  errors.New("migration failed: ERROR: could not serialize access (SQLSTATE 40001)"),
			want: true,
		},
		{
			name: "timescale concurrent catalog delete",
			err:  errors.New("migration failed: ERROR: tuple concurrently deleted (SQLSTATE XX000)"),
			want: true,
		},
		{
			name: "generic internal postgres error",
			err:  errors.New("migration failed: ERROR: cache lookup failed (SQLSTATE XX000)"),
			want: false,
		},
		{
			name: "non-retryable migration error",
			err:  errors.New("migration failed: ERROR: syntax error (SQLSTATE 42601)"),
			want: false,
		},
		{
			name: "unique violation is not retried",
			err:  errors.New("migration failed: ERROR: duplicate key value (SQLSTATE 23505)"),
			want: false,
		},
		// Transient server restart/recovery: a TimescaleDB crash under
		// concurrent-migration load comes back within a second or two, so these
		// retry rather than cascade into a job-wide failure.
		{
			name: "server not yet accepting connections (57P03)",
			err:  errors.New("failed to connect: FATAL: the database system is not yet accepting connections (SQLSTATE 57P03)"),
			want: true,
		},
		{
			name: "bad connection mid-migration",
			err:  errors.New("failed to run migrations: CREATE INDEX CONCURRENTLY foo (details: driver: bad connection)"),
			want: true,
		},
		{
			name: "connection already closed",
			err:  errors.New("sql: connection is already closed in line 0: SELECT pg_advisory_unlock($1)"),
			want: true,
		},
		{
			name: "server starting up",
			err:  errors.New("FATAL: the database system is starting up"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableMigrationError(tt.err); got != tt.want {
				t.Fatalf("isRetryableMigrationError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
