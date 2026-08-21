package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// TestTemplateNamePatternOnlyMatchesOwnTemplates guards the cleanup filter. A
// LIKE pattern cannot be used here: every underscore in `fleet_test_tmpl_%` is a
// single-character wildcard, so LIKE matches unrelated databases that this
// harness must never drop.
func TestTemplateNamePatternOnlyMatchesOwnTemplates(t *testing.T) {
	matching := []string{
		"fleet_test_tmpl_0123456789ab",
		"fleet_test_tmpl_deadbeef1234",
	}
	notMatching := []string{
		"fleet",                             // the real database
		"fleetXtestYtmplZproduction",        // matches LIKE, must not match here
		"fleet_test_tmplXdeadbeef1234",      // ditto
		"fleet_test_tmpl_deadbeef1234_prod", // suffixed
		"xfleet_test_tmpl_deadbeef1234",     // prefixed
		"fleet_test_tmpl_DEADBEEF1234",      // uppercase hex is not what we emit
		"fleet_test_tmpl_zzzzzzzzzzzz",      // non-hex
		"fleet_test_tmpl_dead",              // too short
		`fleet_test_tmpl_"; DROP DATABASE fleet; --`,
		"fleet_test_somestore_ab12", // a per-test database
	}

	for _, name := range matching {
		if !templateNamePattern.MatchString(name) {
			t.Errorf("templateNamePattern should match own template %q", name)
		}
	}
	for _, name := range notMatching {
		if templateNamePattern.MatchString(name) {
			t.Errorf("templateNamePattern must not match %q", name)
		}
	}

	// The name we actually generate has to satisfy its own pattern.
	if got := templateDBPrefix + migrationSetFingerprint(); !templateNamePattern.MatchString(got) {
		t.Errorf("generated template name %q does not match templateNamePattern", got)
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: "fleet_test_tmpl_deadbeef1234", want: `"fleet_test_tmpl_deadbeef1234"`},
		{in: `weird"name`, want: `"weird""name"`},
		{in: `"; DROP DATABASE fleet; --`, want: `"""; DROP DATABASE fleet; --"`},
	}

	for _, tt := range tests {
		if got := quoteIdentifier(tt.in); got != tt.want {
			t.Errorf("quoteIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsMissingTemplateError(t *testing.T) {
	missing := `create test database: ERROR: database "fleet_test_tmpl_abc" does not exist (SQLSTATE 3D000)`

	if !isMissingTemplateError(missing, "fleet_test_tmpl_abc") {
		t.Error("a swept template must be recognised so the clone can rebuild it")
	}
	if isMissingTemplateError(missing, "") {
		t.Error("without a template there is nothing to rebuild")
	}
	if isMissingTemplateError("create test database: ERROR: permission denied (SQLSTATE 42501)", "tmpl") {
		t.Error("unrelated failures must not be treated as a missing template")
	}
	// A missing template is not in the generic retry set: it needs the rebuild
	// path, and retrying the same CREATE would fail identically.
	if isRetryableCreateError(missing) {
		t.Error("missing template should be handled by rebuild, not blind retry")
	}
}

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

// adminConfigForTest builds the "postgres" admin config the harness itself uses.
func adminConfigForTest(t *testing.T) *db.Config {
	t.Helper()

	cli := struct {
		DB db.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	assert.NoError(t, err)
	_, err = parser.Parse(nil)
	assert.NoError(t, err)

	config := cli.DB
	config.Name = "postgres"
	return &config
}

// TestDropStaleTemplateDatabasesOnlySweepsOldOwnedTemplates plants one database
// per category cleanup has to distinguish and asserts only a genuinely old,
// owned template is dropped.
func TestDropStaleTemplateDatabasesOnlySweepsOldOwnedTemplates(t *testing.T) {
	const (
		stale       = "fleet_test_tmpl_deadbeef1234" // ours, past the age gate: drop
		fresh       = "fleet_test_tmpl_deadbeef5678" // ours, another run's live one: keep
		uncommented = "fleet_test_tmpl_deadbeef9abc" // right shape, not ours: keep
		lookalike   = "fleet_test_tmplxdeadbeef1234" // matches LIKE only: keep
	)
	fixtures := []string{stale, fresh, uncommented, lookalike}

	adminConfig := adminConfigForTest(t)
	ctx := t.Context()

	adminDB, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer adminDB.Close()

	adminConn, err := adminDB.Conn(ctx)
	assert.NoError(t, err)
	defer adminConn.Close()

	for _, name := range fixtures {
		_, _ = adminConn.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
		_, err = adminConn.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(name))
		assert.NoError(t, err, "creating fixture database %s", name)
	}
	t.Cleanup(func() {
		// A fresh connection: the deferred adminDB.Close() above already ran by
		// the time cleanups fire, so reusing it would silently leak the fixtures.
		// nolint: usetesting
		cleanupCtx := context.Background()
		cleanupDB, err := db.ConnectToDatabase(adminConfig)
		assert.NoError(t, err)
		defer cleanupDB.Close()

		for _, name := range fixtures {
			_, err := cleanupDB.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
			assert.NoError(t, err, "dropping fixture database %s", name)
		}
	})

	stamp := func(name string, created time.Time) {
		_, err := adminConn.ExecContext(ctx, fmt.Sprintf("COMMENT ON DATABASE %s IS %s",
			quoteIdentifier(name),
			quoteLiteral(templateCommentPrefix+created.UTC().Format(time.RFC3339Nano))))
		assert.NoError(t, err, "stamping fixture database %s", name)
	}
	stamp(stale, time.Now().Add(-templateStaleAfter-time.Hour))
	stamp(fresh, time.Now().Add(-time.Minute))

	// Keep the template belonging to the current migration set, as a real run would.
	dropStaleTemplateDatabases(ctx, adminConn, templateDBPrefix+migrationSetFingerprint())

	assert.False(t, databaseExists(t, adminConn, stale),
		"template %s is ours and past the age gate; it should have been dropped", stale)
	assert.True(t, databaseExists(t, adminConn, fresh),
		"template %s is recent, so a concurrent run may still be cloning it; sweeping it causes rebuild storms", fresh)
	assert.True(t, databaseExists(t, adminConn, uncommented),
		"database %s carries no ownership metadata and must not be dropped", uncommented)
	assert.True(t, databaseExists(t, adminConn, lookalike),
		"database %s only matches the wildcard-underscore LIKE pattern and must not be dropped", lookalike)
}

func TestTemplateIsStale(t *testing.T) {
	stamp := func(created time.Time) sql.NullString {
		return sql.NullString{
			String: templateCommentPrefix + created.UTC().Format(time.RFC3339Nano),
			Valid:  true,
		}
	}

	if !templateIsStale(stamp(time.Now().Add(-templateStaleAfter - time.Minute))) {
		t.Error("a template older than the age gate should be sweepable")
	}
	if templateIsStale(stamp(time.Now().Add(-time.Minute))) {
		t.Error("a recent template may belong to a concurrent run and must be kept")
	}
	if templateIsStale(sql.NullString{}) {
		t.Error("a database with no comment is not ours to drop")
	}
	if templateIsStale(sql.NullString{String: "someone else's database", Valid: true}) {
		t.Error("a foreign comment is not ours to drop")
	}
	if templateIsStale(sql.NullString{String: templateCommentPrefix + "not-a-timestamp", Valid: true}) {
		t.Error("an unparseable timestamp should be left alone rather than guessed about")
	}
}

// TestGetTestDBRecoversFromSweptTemplate simulates a concurrent run on a
// different migration set sweeping our template between clones: the next test
// must rebuild it rather than fail with "database does not exist" (SQLSTATE
// 3D000), which is not in the generic retry set.
func TestGetTestDBRecoversFromSweptTemplate(t *testing.T) {
	// Establishes the template as a side effect.
	_ = GetTestDB(t)

	templateMu.Lock()
	prepared, name := templatePrepared, templateDBName
	templateMu.Unlock()

	if !prepared {
		t.Skip("no template was prepared; nothing to sweep")
	}

	adminConfig := adminConfigForTest(t)
	adminDB, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer adminDB.Close()

	adminConn, err := adminDB.Conn(t.Context())
	assert.NoError(t, err)
	defer adminConn.Close()

	_, err = adminConn.ExecContext(t.Context(), "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
	assert.NoError(t, err, "sweeping the template out from under the harness")
	assert.False(t, databaseExists(t, adminConn, name), "template should be gone before the next clone")

	// Must succeed despite the template having vanished mid-run.
	conn := GetTestDB(t)
	var one int
	assert.NoError(t, conn.QueryRowContext(t.Context(), "SELECT 1").Scan(&one))
	assert.Equal(t, 1, one)

	assert.True(t, databaseExists(t, adminConn, name), "template should have been rebuilt")
}

func databaseExists(t *testing.T, adminConn *sql.Conn, name string) bool {
	t.Helper()

	var exists bool
	err := adminConn.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists)
	assert.NoError(t, err, fmt.Sprintf("checking whether %s exists", name))
	return exists
}
