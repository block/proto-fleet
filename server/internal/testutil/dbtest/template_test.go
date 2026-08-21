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

// TestCreateTestDatabaseRecoversFromMissingTemplate exercises the recovery path
// for a template that vanished mid-run (a concurrent checkout on a different
// migration set sweeping it, say). It names a private template that never
// existed rather than dropping the live one, which every other package and
// checkout on this server is cloning: dropping that would race their clones and
// force them to rebuild or replay migrations.
func TestCreateTestDatabaseRecoversFromMissingTemplate(t *testing.T) {
	const missing = "fleet_test_tmpl_ffffffffffff" // right shape, never created

	adminConfig := adminConfigForTest(t)
	dbName := generateTestDBName(t.Name())

	// Recovery must not fail the test: the missing template is diagnosed,
	// discarded, and replaced by whatever templateDatabase can offer (the real
	// template, or "" to migrate directly).
	used := createTestDatabase(t, adminConfig, dbName, missing)
	assert.NotEqual(t, missing, used, "a missing template should have been replaced")

	adminDB, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer adminDB.Close()

	adminConn, err := adminDB.Conn(t.Context())
	assert.NoError(t, err)
	defer adminConn.Close()

	t.Cleanup(func() {
		// nolint: usetesting
		cleanupCtx := context.Background()
		cleanupDB, err := db.ConnectToDatabase(adminConfig)
		assert.NoError(t, err)
		defer cleanupDB.Close()
		_, err = cleanupDB.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName))
		assert.NoError(t, err)
	})

	assert.True(t, databaseExists(t, adminConn, dbName),
		"the test database should have been created despite the missing template")
	assert.False(t, databaseExists(t, adminConn, missing),
		"recovery must not resurrect a template that never existed")
}

// TestInvalidateTemplateDatabaseOnlyForgetsTheNamedTemplate makes sure recovery
// for one template cannot discard a different, still-valid cached one.
func TestInvalidateTemplateDatabaseOnlyForgetsTheNamedTemplate(t *testing.T) {
	templateMu.Lock()
	savedName, savedPrepared := templateDBName, templatePrepared
	templateDBName, templatePrepared = "fleet_test_tmpl_aaaaaaaaaaaa", true
	templateMu.Unlock()

	t.Cleanup(func() {
		templateMu.Lock()
		templateDBName, templatePrepared = savedName, savedPrepared
		templateMu.Unlock()
	})

	invalidateTemplateDatabase("fleet_test_tmpl_bbbbbbbbbbbb")

	templateMu.Lock()
	name, prepared := templateDBName, templatePrepared
	templateMu.Unlock()

	assert.True(t, prepared, "invalidating another name must not clear the cached template")
	assert.Equal(t, "fleet_test_tmpl_aaaaaaaaaaaa", name)

	invalidateTemplateDatabase("fleet_test_tmpl_aaaaaaaaaaaa")

	templateMu.Lock()
	prepared = templatePrepared
	templateMu.Unlock()

	assert.False(t, prepared, "invalidating the cached name must clear it")
}

func databaseExists(t *testing.T, adminConn *sql.Conn, name string) bool {
	t.Helper()

	var exists bool
	err := adminConn.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists)
	assert.NoError(t, err, fmt.Sprintf("checking whether %s exists", name))
	return exists
}

// TestPrepareAttemptTimeoutStaysWithinProcessBudget pins the arithmetic that
// keeps preparation from outliving Go's package timeout: attempts share one
// process-wide budget instead of each getting a fresh deadline.
func TestPrepareAttemptTimeoutStaysWithinProcessBudget(t *testing.T) {
	now := time.Now()

	// Plenty of budget left: capped by the per-attempt limit.
	got, ok := prepareAttemptTimeout(now.Add(templatePrepareBudget), now)
	assert.True(t, ok)
	assert.Equal(t, templatePrepareTimeout, got)

	// Nearly spent: the remaining budget wins, never the per-attempt cap.
	got, ok = prepareAttemptTimeout(now.Add(5*time.Second), now)
	assert.True(t, ok)
	assert.Equal(t, 5*time.Second, got)

	// Spent, exactly and then past: no further attempts.
	_, ok = prepareAttemptTimeout(now, now)
	assert.False(t, ok)
	_, ok = prepareAttemptTimeout(now.Add(-time.Minute), now)
	assert.False(t, ok)

	// The worst case must stay clear of Go's default 10m package timeout: the
	// budget bounds *all* attempts, so it is the only number that matters.
	assert.True(t, templatePrepareBudget < 10*time.Minute,
		"preparation budget must leave room for the fallback path to run")
	assert.True(t, templatePrepareTimeout <= templatePrepareBudget,
		"a single attempt must not be able to consume more than the whole budget")
}

func TestTemplateCreatedAt(t *testing.T) {
	want := time.Now().UTC().Truncate(time.Millisecond)
	got, ok := templateCreatedAt(sql.NullString{
		String: templateCommentPrefix + want.Format(time.RFC3339Nano),
		Valid:  true,
	})
	assert.True(t, ok)
	assert.Equal(t, want, got.UTC())

	for _, comment := range []sql.NullString{
		{},
		{String: "someone else's database", Valid: true},
		{String: templateCommentPrefix + "not-a-timestamp", Valid: true},
	} {
		if _, ok := templateCreatedAt(comment); ok {
			t.Errorf("templateCreatedAt(%v) should not have parsed", comment)
		}
	}
}

// TestTemplateAgeBoundsAreConsistent pins the relationship between reuse and
// cleanup: a template must stop being reused long before another run may sweep
// it, or a live template could be swept while still considered current.
func TestTemplateAgeBoundsAreConsistent(t *testing.T) {
	assert.True(t, templateMaxAge < templateStaleAfter,
		"reuse window (%s) must be shorter than the sweep threshold (%s)", templateMaxAge, templateStaleAfter)
}

// TestEvictTemplateSessionsSpareRebuildsButClearStrays is the regression test for
// the interaction between clone recovery and in-place rebuilds: a clone that
// loses the race to an in-progress rebuild must not terminate that rebuild's
// migration, while a stray session on a published template must still be cleared
// (otherwise every clone fails, which is what the TimescaleDB background worker
// caused originally).
func TestEvictTemplateSessionsSpareRebuildsButClearStrays(t *testing.T) {
	const name = "fleet_test_tmpl_cccccccccccc"

	adminConfig := adminConfigForTest(t)
	ctx := t.Context()

	adminDB, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer adminDB.Close()

	_, _ = adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
	_, err = adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(name))
	assert.NoError(t, err)
	t.Cleanup(func() {
		// nolint: usetesting
		cleanupCtx := context.Background()
		cleanupDB, err := db.ConnectToDatabase(adminConfig)
		assert.NoError(t, err)
		defer cleanupDB.Close()
		_, _ = cleanupDB.ExecContext(cleanupCtx,
			"ALTER DATABASE "+quoteIdentifier(name)+" WITH ALLOW_CONNECTIONS true")
		_, err = cleanupDB.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
		assert.NoError(t, err)
	})

	// Stand in for the migration session of a rebuild in progress.
	rebuildConfig := *adminConfig
	rebuildConfig.Name = name
	rebuildDB, err := db.ConnectToDatabase(&rebuildConfig)
	assert.NoError(t, err)
	defer rebuildDB.Close()

	rebuildConn, err := rebuildDB.Conn(ctx)
	assert.NoError(t, err)
	defer rebuildConn.Close()

	var alive int
	assert.NoError(t, rebuildConn.QueryRowContext(ctx, "SELECT 1").Scan(&alive))

	// Unsealed: a build is in progress, so nothing may be evicted.
	assert.True(t, templateRebuildInProgress(ctx, adminConfig, name),
		"an unsealed template should read as a rebuild in progress")
	evictTemplateSessions(ctx, adminConfig, name)

	assert.NoError(t, rebuildConn.QueryRowContext(ctx, "SELECT 1").Scan(&alive),
		"eviction must not terminate the migration session of an in-progress rebuild")

	// Sealed: the template is published, so a lingering session is a stray.
	_, err = adminDB.ExecContext(ctx,
		"ALTER DATABASE "+quoteIdentifier(name)+" WITH ALLOW_CONNECTIONS false")
	assert.NoError(t, err)
	assert.False(t, templateRebuildInProgress(ctx, adminConfig, name),
		"a sealed template is not being rebuilt")

	evictTemplateSessions(ctx, adminConfig, name)

	// The terminated session may surface the failure on this query or the next,
	// depending on when the backend notices.
	if err := rebuildConn.QueryRowContext(ctx, "SELECT 1").Scan(&alive); err == nil {
		err = rebuildConn.QueryRowContext(ctx, "SELECT 1").Scan(&alive)
		assert.Error(t, err, "a stray session on a sealed template should have been evicted")
	}
}

func TestTemplateRebuildInProgressForMissingTemplate(t *testing.T) {
	adminConfig := adminConfigForTest(t)

	assert.False(t, templateRebuildInProgress(t.Context(), adminConfig, "fleet_test_tmpl_ffffffffffff"),
		"a template that does not exist is not being rebuilt")
	assert.False(t, templateRebuildInProgress(t.Context(), adminConfig, ""),
		"no template means nothing to wait for")
}
