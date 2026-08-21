package dbtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/migrations"
)

// The per-test database harness used to replay every migration into every test
// database. With 140+ migrations and hundreds of DB-backed tests that cost
// ~2s per test and dominated the package runtime (the sqlstores package spent
// ~500s of its 600s Go test timeout inside migrate.Up()).
//
// Instead we migrate once into a template database and let PostgreSQL clone it
// at the filesystem level for each test (CREATE DATABASE ... TEMPLATE), which
// is O(schema size) rather than O(number of migrations) and takes tens of
// milliseconds.
//
// The template is keyed by a fingerprint of the embedded migration set, so
// adding or editing a migration transparently builds a new template and
// abandons the old one. Preparation is guarded by a PostgreSQL advisory lock so
// that the many test binaries `go test ./...` runs in parallel cooperate: the
// first one builds the template, the rest wait and reuse it.
const (
	// templateDBPrefix namespaces template databases so stale ones from earlier
	// migration sets can be identified and dropped.
	templateDBPrefix = "fleet_test_tmpl_"

	// templateAdvisoryLockKey is an arbitrary fixed key. Any process preparing a
	// template database holds this session-level advisory lock, so concurrent
	// test binaries build the template once rather than racing.
	templateAdvisoryLockKey = int64(7264051893001)

	// templateFingerprintLength is how much of the migration-set hash goes into
	// the template name; 12 hex chars is collision-safe here and keeps the name
	// well inside PostgreSQL's 63-character identifier limit.
	templateFingerprintLength = 12

	// templatePrepareTimeout bounds the whole prepare step (waiting for the
	// advisory lock plus migrating the template).
	templatePrepareTimeout = 10 * time.Minute

	// templateDetachTimeout bounds how long we wait for sessions to leave the
	// template after terminating them.
	templateDetachTimeout  = 30 * time.Second
	templateDetachInterval = 100 * time.Millisecond
)

var (
	templatePrepareOnce sync.Once
	// templateDBName is the prepared template's name, or "" when preparation
	// failed and callers must fall back to migrating each test database.
	templateDBName string
)

// templateDatabase returns the name of a fully migrated template database that
// test databases can be cloned from, preparing it on first use. It returns ""
// if no template could be prepared, in which case the caller should create an
// empty database and migrate it directly.
func templateDatabase(t *testing.T, adminConfig *db.Config) string {
	t.Helper()

	templatePrepareOnce.Do(func() {
		name := templateDBPrefix + migrationSetFingerprint()

		// Deliberately not t.Context(): the template outlives the test that
		// happens to build it, and cancelling that test must not abandon a
		// half-migrated template for every other test in the binary.
		// nolint: usetesting
		ctx, cancel := context.WithTimeout(context.Background(), templatePrepareTimeout)
		defer cancel()

		start := time.Now()
		if err := prepareTemplateDatabase(ctx, adminConfig, name); err != nil {
			t.Logf("could not prepare template database %s (%v); "+
				"falling back to migrating each test database", name, err)
			return
		}
		templateDBName = name
		t.Logf("template database %s ready in %s", name, time.Since(start))
	})

	return templateDBName
}

// prepareTemplateDatabase makes sure a migrated template database exists for the
// current migration set. It is safe to call concurrently from separate
// processes: the advisory lock serialises preparation, and the caller that finds
// a ready template returns immediately.
func prepareTemplateDatabase(ctx context.Context, adminConfig *db.Config, name string) error {
	adminDB, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	defer adminDB.Close()

	// Advisory locks are session-scoped, so pin a single connection rather than
	// letting the pool hand the unlock to a different session.
	adminConn, err := adminDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pin admin connection: %w", err)
	}
	defer adminConn.Close()

	if _, err := adminConn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", templateAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire template advisory lock: %w", err)
	}
	defer func() {
		_, _ = adminConn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", templateAdvisoryLockKey)
	}()

	ready, err := templateDatabaseReady(ctx, adminConn, name)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}

	// Either absent or left half-built by an interrupted run: rebuild it.
	if err := tryDropTestDatabase(ctx, adminConfig, name); err != nil {
		return fmt.Errorf("drop stale template: %w", err)
	}
	if _, err := adminConn.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
		return fmt.Errorf("create template database: %w", err)
	}

	if err := migrateTemplateDatabase(adminConfig, name); err != nil {
		// Leave nothing half-migrated behind for the next run to trust.
		_ = tryDropTestDatabase(ctx, adminConfig, name)
		return err
	}

	// Blocking connections is both a correctness guard and the readiness
	// marker: CREATE DATABASE ... TEMPLATE fails while any session is connected
	// to the source, and a template is only ever published in this state.
	if _, err := adminConn.ExecContext(ctx,
		fmt.Sprintf("ALTER DATABASE %s WITH ALLOW_CONNECTIONS false", name)); err != nil {
		_ = tryDropTestDatabase(ctx, adminConfig, name)
		return fmt.Errorf("seal template database: %w", err)
	}

	// Creating the extension started a TimescaleDB background worker scheduler
	// inside the template, and that counts as a connected session: every clone
	// would fail with "source database is being accessed by other users". Evict
	// it now that ALLOW_CONNECTIONS false stops the launcher reattaching.
	if err := detachTemplateSessions(ctx, adminConn, name); err != nil {
		_ = tryDropTestDatabase(ctx, adminConfig, name)
		return err
	}

	dropStaleTemplateDatabases(ctx, adminConn, name)
	return nil
}

// templateDatabaseReady reports whether a usable template exists. A template is
// usable only once connections have been disabled on it, which happens after
// migrations succeed — so a database that exists but still allows connections is
// treated as incomplete and rebuilt.
func templateDatabaseReady(ctx context.Context, adminConn *sql.Conn, name string) (bool, error) {
	var allowConnections bool
	err := adminConn.QueryRowContext(ctx,
		"SELECT datallowconn FROM pg_database WHERE datname = $1", name).Scan(&allowConnections)
	switch {
	case err == nil:
		return !allowConnections, nil
	case strings.Contains(err.Error(), sql.ErrNoRows.Error()):
		return false, nil
	default:
		return false, fmt.Errorf("inspect template database: %w", err)
	}
}

// migrateTemplateDatabase runs the full migration set against the template and
// closes the connection, leaving no session attached to it.
func migrateTemplateDatabase(adminConfig *db.Config, name string) error {
	templateConfig := *adminConfig
	templateConfig.Name = name

	conn, err := db.ConnectAndMigrate(&templateConfig)
	if err != nil {
		return fmt.Errorf("migrate template database: %w", err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close template connection: %w", err)
	}
	return nil
}

// detachTemplateSessions terminates every backend attached to the template and
// waits for them to disappear, so the first clone does not race the shutdown.
func detachTemplateSessions(ctx context.Context, adminConn *sql.Conn, name string) error {
	deadline := time.Now().Add(templateDetachTimeout)
	for {
		if _, err := adminConn.ExecContext(ctx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, name); err != nil {
			return fmt.Errorf("terminate template sessions: %w", err)
		}

		var attached int
		if err := adminConn.QueryRowContext(ctx,
			"SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			name).Scan(&attached); err != nil {
			return fmt.Errorf("count template sessions: %w", err)
		}
		if attached == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d session(s) still attached to template %s after %s",
				attached, name, templateDetachTimeout)
		}

		time.Sleep(templateDetachInterval)
	}
}

// evictTemplateSessions is the best-effort recovery used when a clone loses the
// race to a session that attached to the template anyway (a background worker
// started before ALLOW_CONNECTIONS false took effect, say). Errors are ignored:
// the caller is already in a retry loop and the CREATE DATABASE it retries
// reports the real failure.
func evictTemplateSessions(ctx context.Context, adminConfig *db.Config, template string) {
	if template == "" {
		return
	}

	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return
	}
	defer conn.Close()

	_, _ = conn.ExecContext(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
	`, template)
}

// dropStaleTemplateDatabases removes templates built for other migration sets
// (typically left by an earlier branch or an older checkout) so they do not
// accumulate on long-lived local databases. Best effort: a template still in use
// by a concurrently running suite simply fails to drop.
func dropStaleTemplateDatabases(ctx context.Context, adminConn *sql.Conn, keep string) {
	rows, err := adminConn.QueryContext(ctx,
		"SELECT datname FROM pg_database WHERE datname LIKE $1 AND datname <> $2",
		templateDBPrefix+"%", keep)
	if err != nil {
		return
	}
	defer rows.Close()

	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return
		}
		stale = append(stale, name)
	}
	if rows.Err() != nil {
		return
	}

	for _, name := range stale {
		// Sealed templates have no sessions to terminate, so a plain DROP is
		// enough; failures mean someone else is mid-clone and we leave it.
		_, _ = adminConn.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", name))
	}
}

// migrationSetFingerprint hashes the embedded migration files (names and
// contents) so that any change to the schema yields a new template database
// rather than silently reusing a stale one.
func migrationSetFingerprint() string {
	hash := sha256.New()

	names, err := migrationFileNames()
	if err != nil {
		// A read failure here would make every test share an unkeyed template;
		// fall back to a unique fingerprint so we build a fresh one instead.
		return fmt.Sprintf("%x", time.Now().UnixNano())[:templateFingerprintLength]
	}

	for _, name := range names {
		contents, err := fs.ReadFile(migrations.Migrations, name)
		if err != nil {
			return fmt.Sprintf("%x", time.Now().UnixNano())[:templateFingerprintLength]
		}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write(contents)
	}

	return hex.EncodeToString(hash.Sum(nil))[:templateFingerprintLength]
}

// migrationFileNames lists the embedded migration files in a stable order.
func migrationFileNames() ([]string, error) {
	entries, err := fs.ReadDir(migrations.Migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
