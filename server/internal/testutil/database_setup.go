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
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
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

	// Now connect to the test database
	testDBConfig := config
	testDBConfig.Name = dbName
	conn, err = db.ConnectAndMigrate(&testDBConfig)
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
