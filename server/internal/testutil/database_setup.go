package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// templateOnce builds a fully-migrated template database exactly once per test
// process. Each subsequent test then provisions its own isolated database as a
// fast `CREATE DATABASE ... TEMPLATE` file-copy instead of replaying every
// migration, which is the dominant per-test cost. Building the template only
// once also means the lock-heavy TimescaleDB continuous-aggregate DDL runs once
// per process rather than once per test, removing the concurrent-migration
// deadlock surface the retry logic below exists to absorb.
var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// GetTestDB creates a test database connection and returns a sql.DB ref for testing.
// The database connection will be closed when the test completes.
func GetTestDB(t *testing.T) *sql.DB {
	t.Helper()

	config := parseTestDBConfig(t)
	adminConfig := config
	adminConfig.Name = "postgres"

	// When the caller pins DB_NAME to a real database we migrate it in place;
	// otherwise we generate a unique name and clone the migrated template.
	useGeneratedName := config.Name == "" || config.Name == "fleet"

	dbName := config.Name
	if useGeneratedName {
		dbName = generateTestDBName(t.Name())
	}

	testDBConfig := config
	testDBConfig.Name = dbName

	var conn *sql.DB
	if useGeneratedName {
		// Fast path: clone a once-migrated template instead of running every
		// migration for this test's database.
		template, err := ensureTemplateDB(t, config, adminConfig)
		assert.NoError(t, err)

		createTestDatabaseFromTemplate(t, &adminConfig, dbName, template)

		conn, err = db.ConnectToDatabase(&testDBConfig)
		assert.NoError(t, err)
	} else {
		// Pinned DB_NAME: create the database and migrate it in place.
		createEmptyTestDatabase(t, &adminConfig, dbName)

		var err error
		// Connect and run migrations with retry on deadlock.
		// TimescaleDB continuous aggregate DDL acquires instance-level catalog
		// locks that can deadlock when parallel tests migrate concurrently. On
		// failure we drop and recreate the database for a clean slate (avoids
		// golang-migrate dirty flag issues).
		conn, err = connectAndMigrateWithRetry(t, &testDBConfig, &adminConfig, dbName)
		assert.NoError(t, err)
	}

	registerTestDatabaseCleanup(t, conn, &adminConfig, dbName)

	return conn
}

// parseTestDBConfig reads the DB config from environment variables the same way
// the server does when it starts.
func parseTestDBConfig(t *testing.T) db.Config {
	t.Helper()

	cli := struct {
		DB db.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	assert.NoError(t, err)
	_, err = parser.Parse(nil)
	assert.NoError(t, err)
	return cli.DB
}

// ensureTemplateDB builds (once per process) a fully-migrated template database
// and returns its name. The migrator connection is closed before returning so
// that CREATE DATABASE ... TEMPLATE, which requires no active sessions on the
// source, succeeds.
func ensureTemplateDB(t *testing.T, config db.Config, adminConfig db.Config) (string, error) {
	t.Helper()

	templateOnce.Do(func() {
		name := generateTemplateDBName()

		createEmptyTestDatabase(t, &adminConfig, name)

		templateConfig := config
		templateConfig.Name = name
		conn, err := connectAndMigrateWithRetry(t, &templateConfig, &adminConfig, name)
		if err != nil {
			templateErr = err
			return
		}
		// Close before cloning: CREATE DATABASE ... TEMPLATE fails if any
		// session is connected to the template.
		if err := conn.Close(); err != nil {
			templateErr = fmt.Errorf("closing template connection: %w", err)
			return
		}
		terminateConnections(t, &adminConfig, name)

		templateName = name
	})

	return templateName, templateErr
}

// createEmptyTestDatabase drops any existing database of the given name and
// creates a fresh empty one.
func createEmptyTestDatabase(t *testing.T, adminConfig *db.Config, dbName string) {
	t.Helper()

	conn, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer conn.Close()

	terminateConnectionsVia(t, conn, dbName)
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	assert.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf("CREATE DATABASE %s", dbName))
	assert.NoError(t, err)
}

// createTestDatabaseFromTemplate drops any existing database of the given name
// and creates a fresh one as a copy of the migrated template.
func createTestDatabaseFromTemplate(t *testing.T, adminConfig *db.Config, dbName, template string) {
	t.Helper()

	conn, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer conn.Close()

	terminateConnectionsVia(t, conn, dbName)
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	assert.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, template))
	assert.NoError(t, err)
}

// registerTestDatabaseCleanup closes the connection and drops the database when
// the test finishes.
func registerTestDatabaseCleanup(t *testing.T, conn *sql.DB, adminConfig *db.Config, dbName string) {
	t.Helper()

	t.Cleanup(func() {
		err := conn.Close()
		assert.NoError(t, err, "error closing db connection")

		adminConn, err := db.ConnectToDatabase(adminConfig)
		assert.NoError(t, err, "error connecting to PostgreSQL")
		defer adminConn.Close()

		// Terminate any remaining connections to the test database.
		// nolint: usetesting
		_, _ = adminConn.ExecContext(context.Background(), fmt.Sprintf(`
			SELECT pg_terminate_backend(pg_stat_activity.pid)
			FROM pg_stat_activity
			WHERE pg_stat_activity.datname = '%s'
			AND pid <> pg_backend_pid()
		`, dbName))

		// nolint: usetesting
		_, err = adminConn.ExecContext(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		assert.NoError(t, err, "error dropping test database")
	})
}

// terminateConnections opens an admin connection and terminates all other
// sessions connected to the given database.
func terminateConnections(t *testing.T, adminConfig *db.Config, dbName string) {
	t.Helper()

	conn, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer conn.Close()
	terminateConnectionsVia(t, conn, dbName)
}

// terminateConnectionsVia terminates all other sessions connected to the given
// database using an existing admin connection.
func terminateConnectionsVia(t *testing.T, conn *sql.DB, dbName string) {
	t.Helper()

	_, _ = conn.ExecContext(t.Context(), fmt.Sprintf(`
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = '%s'
		AND pid <> pg_backend_pid()
	`, dbName))
}

const (
	migrationMaxRetries     = 5
	migrationRetryBaseDelay = 200 * time.Millisecond

	pgInternalError                   = "XX000"
	timescaleTupleConcurrentlyDeleted = "tuple concurrently deleted"
)

// connectAndMigrateWithRetry wraps db.ConnectAndMigrate with retry logic for
// deadlocks caused by concurrent TimescaleDB catalog operations across test
// databases. On deadlock, the test database is dropped and recreated to avoid
// golang-migrate dirty flag issues.
func connectAndMigrateWithRetry(
	t *testing.T,
	testDBConfig *db.Config,
	adminConfig *db.Config,
	dbName string,
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

		t.Logf("migration deadlock (attempt %d/%d), retrying: %v", attempt, migrationMaxRetries, lastErr)
		recreateTestDatabase(t, adminConfig, dbName)

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
	return strings.Contains(msg, db.PGDeadlockDetected) ||
		strings.Contains(msg, db.PGSerializationFailure) ||
		(strings.Contains(msg, pgInternalError) && strings.Contains(msg, timescaleTupleConcurrentlyDeleted))
}

// recreateTestDatabase drops and recreates a test database via the admin connection.
func recreateTestDatabase(t *testing.T, adminConfig *db.Config, dbName string) {
	t.Helper()

	adminConn, err := db.ConnectToDatabase(adminConfig)
	assert.NoError(t, err)
	defer adminConn.Close()

	_, _ = adminConn.ExecContext(t.Context(), fmt.Sprintf(`
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = '%s'
		AND pid <> pg_backend_pid()
	`, dbName))

	_, err = adminConn.ExecContext(t.Context(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	assert.NoError(t, err)
	_, err = adminConn.ExecContext(t.Context(), fmt.Sprintf("CREATE DATABASE %s", dbName))
	assert.NoError(t, err)
}

// generateTemplateDBName creates a per-process template database name. The PID
// keeps it unique across the test binaries that `go test` runs concurrently, so
// each process owns its own template on the shared instance.
func generateTemplateDBName() string {
	return fmt.Sprintf("fleet_test_template_%d", os.Getpid())
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
