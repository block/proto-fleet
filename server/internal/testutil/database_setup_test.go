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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableMigrationError(tt.err); got != tt.want {
				t.Fatalf("isRetryableMigrationError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
