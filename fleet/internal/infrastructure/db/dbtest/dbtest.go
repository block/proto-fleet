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

	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
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

	// Connect to MySQL without selecting a database to create our test database
	config.Name = ""
	conn, err := db.ConnectToDatabase(&config)
	assert.NoError(t, err)

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
		// Reconnect to MySQL without selecting a database to drop the test database
		conn, err = db.ConnectToDatabase(&config)
		assert.NoError(t, err, "error connecting to MySQL")
		defer conn.Close()
		// nolint: usetesting
		_, err = conn.ExecContext(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		assert.NoError(t, err, "error dropping test database")
	})

	return conn
}

// generateTestDBName creates a unique database name that includes part of the test name for readability.
// MySQL has a 64 character limit for database names.
// Format: fleet_test_<test_name>_<4 chars of random suffix>
func generateTestDBName(testName string) string {
	// Get a readable part of the test name, removing any special characters
	sanitizedName := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, testName)

	// Truncate test name to leave room for prefix, suffix and random suffix
	// fleet_test_ (12 chars) + _ (1 char) + random (4 chars) = 17 chars reserved
	maxTestNameLength := 64 - 17
	if len(sanitizedName) > maxTestNameLength {
		sanitizedName = sanitizedName[:maxTestNameLength]
	}

	// Use last 16 bits of UnixNano for uniqueness (4 hex chars)
	randomSuffix := fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)

	return fmt.Sprintf("fleet_test_%s_%s", sanitizedName, randomSuffix)
}
