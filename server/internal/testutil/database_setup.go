package testutil

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

	// Connect to PostgreSQL without selecting a database to create our test database
	// Connect to the default "postgres" database first
	adminConfig := config
	adminConfig.Name = "postgres"
	conn, err := db.ConnectToDatabase(&adminConfig)
	assert.NoError(t, err)

	// Drop existing connections to the test database if any
	_, _ = conn.ExecContext(t.Context(), fmt.Sprintf(`
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = '%s'
		AND pid <> pg_backend_pid()
	`, dbName))

	// Create the test database
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	assert.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf("CREATE DATABASE %s", dbName))
	assert.NoError(t, err)
	conn.Close()

	// Connect and run migrations with retry on deadlock.
	// TimescaleDB continuous aggregate DDL acquires instance-level catalog locks
	// that can deadlock when parallel tests migrate concurrently. On failure we
	// drop and recreate the database for a clean slate (avoids golang-migrate
	// dirty flag issues).
	testDBConfig := config
	testDBConfig.Name = dbName
	conn, err = connectAndMigrateWithRetry(t, &testDBConfig, &adminConfig, dbName)
	assert.NoError(t, err)

	// Clean up the database when the test is done
	t.Cleanup(func() {
		err := conn.Close()
		assert.NoError(t, err, "error closing db connection")
		// Reconnect to the default "postgres" database to drop the test database
		conn, err = db.ConnectToDatabase(&adminConfig)
		assert.NoError(t, err, "error connecting to PostgreSQL")
		defer conn.Close()

		// Terminate any remaining connections to the test database
		// nolint: usetesting
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf(`
			SELECT pg_terminate_backend(pg_stat_activity.pid)
			FROM pg_stat_activity
			WHERE pg_stat_activity.datname = '%s'
			AND pid <> pg_backend_pid()
		`, dbName))

		// nolint: usetesting
		_, err = conn.ExecContext(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		assert.NoError(t, err, "error dropping test database")
	})

	return conn
}

const (
	migrationMaxRetries     = 5
	migrationRetryBaseDelay = 200 * time.Millisecond
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
	if db.IsRetryablePostgresError(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, db.PGDeadlockDetected) || strings.Contains(msg, db.PGSerializationFailure)
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
