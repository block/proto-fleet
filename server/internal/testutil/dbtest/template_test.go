package dbtest

import (
	"strings"
	"testing"
)

func TestIsRetryableCreateError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "template in use sqlstate",
			msg:  `create test database: ERROR: source database "fleet_test_tmpl_abc" is being accessed by other users (SQLSTATE 55006)`,
			want: true,
		},
		{
			name: "object in use sqlstate only",
			msg:  "create test database: ERROR: something is in use (SQLSTATE 55006)",
			want: true,
		},
		{
			name: "in-use text without sqlstate",
			msg:  "create test database: ERROR: database is being accessed by other users",
			want: true,
		},
		{
			name: "transient server restart still retryable",
			msg:  "connect to admin database: the database system is starting up",
			want: true,
		},
		{
			name: "permission failure is not retryable",
			msg:  "create test database: ERROR: permission denied to create database (SQLSTATE 42501)",
			want: false,
		},
		{
			name: "duplicate database is not retryable",
			msg:  `create test database: ERROR: database "fleet_test_x" already exists (SQLSTATE 42P04)`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableCreateError(tt.msg); got != tt.want {
				t.Errorf("isRetryableCreateError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// TestMigrationSetFingerprintIsStableAndNamesAValidIdentifier guards the two
// properties the template name depends on: repeated calls in a run must agree
// (otherwise every test binary builds its own template) and the resulting
// database name must fit PostgreSQL's identifier limit.
func TestMigrationSetFingerprintIsStableAndNamesAValidIdentifier(t *testing.T) {
	first := migrationSetFingerprint()
	second := migrationSetFingerprint()

	if first != second {
		t.Errorf("fingerprint not stable across calls: %q vs %q", first, second)
	}
	if len(first) != templateFingerprintLength {
		t.Errorf("fingerprint length = %d, want %d", len(first), templateFingerprintLength)
	}
	if strings.Trim(first, "0123456789abcdef") != "" {
		t.Errorf("fingerprint %q is not lowercase hex", first)
	}

	name := templateDBPrefix + first
	if len(name) > 63 {
		t.Errorf("template database name %q is %d chars, exceeds PostgreSQL's 63-char limit", name, len(name))
	}
}

// TestMigrationFileNamesAreSortedAndNonEmpty pins the ordering the fingerprint
// relies on: an unstable listing order would change the hash between processes
// and defeat template reuse.
func TestMigrationFileNamesAreSortedAndNonEmpty(t *testing.T) {
	names, err := migrationFileNames()
	if err != nil {
		t.Fatalf("migrationFileNames() error = %v", err)
	}
	if len(names) == 0 {
		t.Fatal("migrationFileNames() returned no files; the embedded migration set should not be empty")
	}

	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("migration file names not strictly sorted at %d: %q >= %q", i, names[i-1], names[i])
		}
	}
}
