// Package dbtest provides the per-test database harness: it creates a unique
// database per test by cloning a migrated template database (see template.go),
// and drops it at cleanup, retrying each step across transient server restarts
// (crash-recovery under heavy concurrent migration load). It lives in a leaf
// package — importing only
// infrastructure/db — so tests in packages that testutil itself imports
// (e.g. domain/command) can use the same hardened harness without an import
// cycle. Most tests should keep using testutil.GetTestDB, which delegates here.
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

// GetTestDB creates a test database connection and returns a sql.DB ref for testing.
// The database connection will be closed when the test completes.
func GetTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Parse the DB config from environment variables the same way we would when
	// running the server.
	cli := struct {
		DB db.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	assert.NoError(t, err)
	_, err = parser.Parse(nil)
	assert.NoError(t, err)
	config := cli.DB
	dbName := config.Name
	if dbName == "" || dbName == "fleet" {
		// If the DB name is not set, or is the default name, generate a unique name
		dbName = generateTestDBName(t.Name())
	}

	// Create the test database on the default "postgres" database.
	// createTestDatabase waits out / retries a transient server restart, so a
	// test that starts while the server is still recovering does not fail on the
	// admin DDL before reaching the migration retry below.
	adminConfig := config
	adminConfig.Name = "postgres"

	// Clone the migrated template so this test skips the migration replay
	// entirely. Falls back to an empty database (migrated below) when no
	// template could be prepared.
	template := createTestDatabase(t, &adminConfig, dbName, templateDatabase(t, &adminConfig))

	// Connect and run migrations with retry. Cloned databases are already at the
	// latest version, so this is a no-op version check that also self-heals a
	// stale clone; without a template it runs the full migration set. TimescaleDB
	// continuous aggregate DDL acquires instance-level catalog locks that can
	// deadlock when parallel tests migrate concurrently; a transient server
	// restart is also tolerated. On failure we drop and recreate the database for
	// a clean slate (avoids golang-migrate dirty flag issues).
	testDBConfig := config
	testDBConfig.Name = dbName
	conn, err := connectAndMigrateWithRetry(t, &testDBConfig, &adminConfig, dbName, template)
	assert.NoError(t, err)

	// Clean up the database when the test is done
	t.Cleanup(func() {
		err := conn.Close()
		assert.NoError(t, err, "error closing db connection")
		// Cleanup runs after t.Context() is canceled, so use Background.
		// nolint: usetesting
		dropTestDatabase(context.Background(), t, &adminConfig, dbName)
	})

	return conn
}

const (
	migrationMaxRetries     = 5
	migrationRetryBaseDelay = 200 * time.Millisecond

	pgInternalError                   = "XX000"
	pgObjectInUse                     = "55006"
	pgUndefinedDatabase               = "3D000"
	timescaleTupleConcurrentlyDeleted = "tuple concurrently deleted"
	templateSourceInUse               = "is being accessed by other users"

	// serverReadyTimeout bounds how long we wait for the database server to
	// start accepting connections again after a transient restart before a
	// migration retry.
	serverReadyTimeout  = 30 * time.Second
	serverReadyInterval = 250 * time.Millisecond
)

// transientServerErrors are substrings of errors emitted while the database
// server itself is restarting/recovering — e.g. a crash under heavy concurrent
// migration load that `restart: always` brings back within a second or two.
// Under the per-test-database model one such restart otherwise cascades into
// dozens of unrelated failures; retrying once the server is back lets each test
// survive the blip instead.
var transientServerErrors = []string{
	// PostgreSQL class 57 (operator_intervention): the server is shutting
	// down, crashing, or not yet back up — all expected during a restart.
	"57P01", // admin_shutdown: "terminating connection due to administrator command"
	"57P02", // crash_shutdown
	"57P03", // cannot_connect_now: "the database system is not yet accepting connections"
	"the database system is starting up",
	"the database system is shutting down",
	"the database system is in recovery mode",
	// Connection torn down mid-statement when the server went away.
	"bad connection",
	"connection is already closed",
	"connection reset by peer",
	// pgx surfaces a mid-crash disconnect as "failed to receive
	// message: unexpected EOF" and a fully-down server as ECONNREFUSED.
	"unexpected EOF",
	"connection refused",
	"broken pipe",
}

// connectAndMigrateWithRetry wraps db.ConnectAndMigrate with retry logic for
// deadlocks caused by concurrent TimescaleDB catalog operations across test
// databases. On deadlock, the test database is dropped and recreated to avoid
// golang-migrate dirty flag issues.
func connectAndMigrateWithRetry(
	t *testing.T,
	testDBConfig *db.Config,
	adminConfig *db.Config,
	dbName string,
	template string,
) (*sql.DB, error) {
	t.Helper()

	var conn *sql.DB
	var lastErr error
	for attempt := 1; attempt <= migrationMaxRetries; attempt++ {
		conn, lastErr = db.ConnectAndMigrate(testDBConfig)
		if lastErr == nil {
			return conn, nil
		}

		if !isRetryableMigrationError(lastErr) || attempt == migrationMaxRetries {
			return nil, lastErr
		}

		t.Logf("retryable migration error (attempt %d/%d), retrying: %v", attempt, migrationMaxRetries, lastErr)
		// Recreate the database for a clean slate (clears any dirty migration
		// state), waiting out / retrying a transient server restart.
		template = createTestDatabase(t, adminConfig, dbName, template)

		delay := time.Duration(attempt) * migrationRetryBaseDelay
		time.Sleep(delay)
	}

	return nil, lastErr
}

// isRetryableMigrationError checks whether a migration error is caused by a
// deadlock or serialization failure. golang-migrate wraps database errors as
// strings, so we check for SQLSTATE codes in the message text.
func isRetryableMigrationError(err error) bool {
	if err == nil {
		return false
	}
	if db.IsRetryablePostgresError(err) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, db.PGDeadlockDetected) ||
		strings.Contains(msg, db.PGSerializationFailure) ||
		(strings.Contains(msg, pgInternalError) && strings.Contains(msg, timescaleTupleConcurrentlyDeleted)) {
		return true
	}
	return isTransientServerError(msg)
}

// isTransientServerError reports whether the error came from the database server
// being temporarily unavailable (restarting/recovering) rather than from the
// migration itself.
func isTransientServerError(msg string) bool {
	for _, s := range transientServerErrors {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// waitForServerReady blocks until the database server accepts a query on the
// admin database, or serverReadyTimeout elapses, or ctx is canceled. This
// bridges the brief window after a transient server restart so the admin DDL
// used to recreate or drop a test database runs against a live server.
func waitForServerReady(ctx context.Context, t *testing.T, adminConfig *db.Config) {
	t.Helper()

	deadline := time.Now().Add(serverReadyTimeout)
	for {
		err := pingAdminDatabase(ctx, adminConfig)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			// The caller's context is gone (e.g. t.Context() after a test
			// timeout) — every further ping would fail immediately, so don't
			// spin out the remaining deadline.
			t.Logf("context done while waiting for database server: %v", err)
			return
		}
		if time.Now().After(deadline) {
			// Give up waiting and let the caller's admin DDL surface the real
			// failure, but log why readiness never confirmed to aid diagnosis.
			t.Logf("database server not ready after %s: %v", serverReadyTimeout, err)
			return
		}
		time.Sleep(serverReadyInterval)
	}
}

// pingAdminDatabase opens a fresh admin connection and runs SELECT 1, returning
// an error while the server is not accepting queries (connect or ping failure).
func pingAdminDatabase(ctx context.Context, adminConfig *db.Config) error {
	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	defer conn.Close()

	pingCtx, cancel := context.WithTimeout(ctx, serverReadyInterval)
	defer cancel()
	if _, err := conn.ExecContext(pingCtx, "SELECT 1"); err != nil {
		return fmt.Errorf("ping admin database: %w", err)
	}
	return nil
}

// createTestDatabase drops any existing database with the given name and
// creates a fresh one, cloning template when non-empty. It waits for the server
// to accept connections and retries the admin DDL across a transient server
// restart (the same 57P03 / "bad connection" / startup window
// connectAndMigrateWithRetry tolerates), so a test that starts while the server
// is recovering survives the blip instead of failing on the very first
// DROP/CREATE.
//
// It returns the template actually used, which can differ from the one passed
// in: a template swept by a concurrent run on a different migration set is
// rebuilt (or given up on, yielding "") so the caller migrates the database
// itself.
func createTestDatabase(t *testing.T, adminConfig *db.Config, dbName string, template string) string {
	t.Helper()

	var lastErr error
	for attempt := 1; attempt <= migrationMaxRetries; attempt++ {
		// Wait for the server before issuing admin DDL.
		waitForServerReady(t.Context(), t, adminConfig)

		lastErr = tryCreateTestDatabase(t.Context(), adminConfig, dbName, template)
		if lastErr == nil {
			return template
		}

		// The template vanished mid-run: another checkout on a different
		// migration set swept it as stale. Rebuild it and try again rather than
		// failing a test for someone else's cleanup.
		if isMissingTemplateError(lastErr.Error(), template) {
			t.Logf("template database %s disappeared, rebuilding: %v", template, lastErr)
			invalidateTemplateDatabase(template)
			template = templateDatabase(t, adminConfig)
			continue
		}

		if !isRetryableCreateError(lastErr.Error()) || attempt == migrationMaxRetries {
			break
		}

		t.Logf("create test database failed transiently (attempt %d/%d), retrying: %v", attempt, migrationMaxRetries, lastErr)

		// A clone is blocked by whatever is attached to the template. If that is
		// a rebuild (the template is not sealed), wait for it to publish: killing
		// it would abort legitimate work, and the retry budget here is far shorter
		// than a migration run. Otherwise the session is a stray — a TimescaleDB
		// worker that slipped in — and gets evicted.
		if templateRebuildInProgress(t.Context(), adminConfig, template) {
			t.Logf("template %s is being rebuilt by another process, waiting for it", template)
			waitForTemplateRebuild(t.Context(), adminConfig, template)
			continue
		}

		evictTemplateSessions(t.Context(), adminConfig, template)
		time.Sleep(time.Duration(attempt) * migrationRetryBaseDelay)
	}

	assert.NoError(t, lastErr, "error creating test database")
	return template
}

// dropTestDatabase drops the test database at cleanup, waiting out and
// retrying a transient server restart the same way createTestDatabase does.
// Without the retry, a server crash-recovery blip elsewhere in the run fails
// an otherwise-passing test at teardown with "the database system is in
// recovery mode" (SQLSTATE 57P03).
func dropTestDatabase(ctx context.Context, t *testing.T, adminConfig *db.Config, dbName string) {
	t.Helper()

	var lastErr error
	for attempt := 1; attempt <= migrationMaxRetries; attempt++ {
		waitForServerReady(ctx, t, adminConfig)

		lastErr = tryDropTestDatabase(ctx, adminConfig, dbName)
		if lastErr == nil {
			return
		}
		if !isTransientServerError(lastErr.Error()) || attempt == migrationMaxRetries {
			break
		}

		t.Logf("drop test database failed transiently (attempt %d/%d), retrying: %v", attempt, migrationMaxRetries, lastErr)
		time.Sleep(time.Duration(attempt) * migrationRetryBaseDelay)
	}

	assert.NoError(t, lastErr, "error dropping test database")
}

// tryDropTestDatabase terminates lingering connections then drops the database
// on a fresh admin connection, returning any error rather than failing the
// test so the caller can retry transient failures.
func tryDropTestDatabase(ctx context.Context, adminConfig *db.Config, dbName string) error {
	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	defer conn.Close()

	// Best effort: lingering connections just make the DROP fail, which the
	// caller retries. The name is bound as a parameter here; the DDL below has
	// to interpolate it, so it goes through quoteIdentifier.
	_, _ = conn.ExecContext(ctx, `
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = $1
		AND pid <> pg_backend_pid()
	`, dbName)

	if _, err := conn.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName)); err != nil {
		return fmt.Errorf("drop test database: %w", err)
	}
	return nil
}

// tryCreateTestDatabase drops any existing database with the given name (after
// terminating lingering connections) and creates a fresh one, returning any
// error rather than failing the test so the caller can retry transient
// failures. When template is non-empty the new database is a clone of it, which
// skips the migration replay.
func tryCreateTestDatabase(ctx context.Context, adminConfig *db.Config, dbName string, template string) error {
	if err := tryDropTestDatabase(ctx, adminConfig, dbName); err != nil {
		return err
	}

	conn, err := db.ConnectToDatabase(adminConfig)
	if err != nil {
		return fmt.Errorf("connect to admin database: %w", err)
	}
	defer conn.Close()

	create := "CREATE DATABASE " + quoteIdentifier(dbName)
	if template != "" {
		create += " TEMPLATE " + quoteIdentifier(template)
	}
	if _, err := conn.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("create test database: %w", err)
	}
	return nil
}

// isRetryableCreateError extends the transient-server check with the contention
// PostgreSQL reports when a database is cloned while another session touches the
// source (SQLSTATE 55006). Parallel tests all clone the same template, so this
// is expected under load rather than a real failure.
func isRetryableCreateError(msg string) bool {
	return isTransientServerError(msg) ||
		strings.Contains(msg, pgObjectInUse) ||
		strings.Contains(msg, templateSourceInUse)
}

// isMissingTemplateError reports whether a clone failed because its source
// template no longer exists (SQLSTATE 3D000). Only meaningful when cloning: the
// database we are creating cannot itself be the undefined one.
func isMissingTemplateError(msg string, template string) bool {
	return template != "" && strings.Contains(msg, pgUndefinedDatabase)
}

// generateTestDBName creates a unique database name that includes part of the test name for readability.
// PostgreSQL has a 63 character limit for identifiers.
// Format: fleet_test_<test_name>_<4 chars of random suffix>
func generateTestDBName(testName string) string {
	// Get a readable part of the test name, removing any special characters
	sanitizedName := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, testName)

	// Convert to lowercase for PostgreSQL compatibility
	sanitizedName = strings.ToLower(sanitizedName)

	// Truncate test name to leave room for prefix, suffix and random suffix
	// fleet_test_ (12 chars) + _ (1 char) + random (4 chars) = 17 chars reserved
	maxTestNameLength := 63 - 17
	if len(sanitizedName) > maxTestNameLength {
		sanitizedName = sanitizedName[:maxTestNameLength]
	}

	// Use last 16 bits of UnixNano for uniqueness (4 hex chars)
	randomSuffix := fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)

	return fmt.Sprintf("fleet_test_%s_%s", sanitizedName, randomSuffix)
}
