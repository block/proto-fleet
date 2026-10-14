package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alecthomas/assert/v2"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/block/proto-fleet/server/migrations"
)

const (
	legacyFixtureMaxAttempts = 3

	pgInternalError                   = "XX000"
	timescaleTupleConcurrentlyDeleted = "tuple concurrently deleted"
)

// newLegacyMigrationTestDB retries only the construction of an empty legacy
// fixture. Upgrades of seeded databases must still run once and fail normally.
func newLegacyMigrationTestDB(t *testing.T, version uint) (*sql.DB, *Config) {
	t.Helper()
	conn, config, err := prepareLegacyMigrationTestDB(t, func(conn *sql.DB, config *Config) error {
		return migrateTestDBSourceTo(t.Context(), conn, config.Name, migrations.Migrations, version)
	})
	assert.NoError(t, err)
	return conn, config
}

func prepareLegacyMigrationTestDB(t *testing.T, initialize func(*sql.DB, *Config) error) (*sql.DB, *Config, error) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		conn, config, discard := createMigrationBridgeTestDB(t)
		err := initialize(conn, config)
		if err == nil {
			return conn, config, nil
		}

		// golang-migrate leaves a dirty marker after a failed migration. Never
		// force it clean or retry in place: discard this disposable fixture.
		discard()
		if !isRetryableLegacyFixtureError(err) || attempt == legacyFixtureMaxAttempts {
			return nil, nil, fmt.Errorf("prepare legacy fixture after %d attempts: %w", attempt, err)
		}
		t.Logf("retryable legacy fixture migration failure (attempt %d/%d), recreating database: %v",
			attempt, legacyFixtureMaxAttempts, err)
		select {
		case <-t.Context().Done():
			return nil, nil, fmt.Errorf("legacy fixture retry cancelled: %w", t.Context().Err())
		case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
		}
	}
}

func isRetryableLegacyFixtureError(err error) bool {
	if err == nil {
		return false
	}
	if isTransientFixtureCause(err) {
		return true
	}
	// golang-migrate stores OrigErr without implementing Unwrap. Inspect that
	// error rather than matching SQLSTATE text in the logged migration SQL.
	var migrationErr migratedatabase.Error
	if errors.As(err, &migrationErr) {
		return isTransientFixtureCause(migrationErr.OrigErr)
	}
	var migrationErrPtr *migratedatabase.Error
	return errors.As(err, &migrationErrPtr) && isTransientFixtureCause(migrationErrPtr.OrigErr)
}

// isTransientFixtureCause also accepts the internal error TimescaleDB raises
// when concurrent test databases race on its catalog, which dbtest retries too.
func isTransientFixtureCause(err error) bool {
	if IsRetryablePostgresError(err) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgInternalError && pgErr.Message == timescaleTupleConcurrentlyDeleted
}

func TestLegacyFixtureDiscardDetachesAttachedSessions(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}
	observer, _ := newMigrationBridgeTestDB(t)
	_, config, discard := createMigrationBridgeTestDB(t)
	attached, err := ConnectToDatabase(config)
	assert.NoError(t, err)
	t.Cleanup(func() { _ = attached.Close() })
	assert.NoError(t, attached.PingContext(t.Context()))

	discard()

	var exists bool
	assert.NoError(t, observer.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", config.Name).Scan(&exists))
	assert.False(t, exists, "discard must drop a database that still has attached sessions")
	assert.Error(t, attached.PingContext(t.Context()))
}

func TestLegacyMigrationFixtureRetries(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}
	for _, test := range []struct {
		name         string
		code         string
		message      string
		failures     int
		wantAttempts int
		wantError    bool
	}{
		{name: "deadlock", code: PGDeadlockDetected, failures: 1, wantAttempts: 2},
		{name: "serialization failure", code: PGSerializationFailure, failures: 1, wantAttempts: 2},
		{name: "timescale catalog race", code: pgInternalError, message: timescaleTupleConcurrentlyDeleted, failures: 1, wantAttempts: 2},
		{name: "permanent failure", code: "23514", failures: 1, wantAttempts: 1, wantError: true},
		{name: "other internal error", code: pgInternalError, failures: 1, wantAttempts: 1, wantError: true},
		{name: "retry limit", code: PGDeadlockDetected, failures: legacyFixtureMaxAttempts, wantAttempts: legacyFixtureMaxAttempts, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			observer, _ := newMigrationBridgeTestDB(t)
			var failedNames []string
			var failedConnections []*sql.DB
			attempts := 0
			conn, config, err := prepareLegacyMigrationTestDB(t, func(conn *sql.DB, config *Config) error {
				attempts++
				for _, name := range failedNames {
					var exists bool
					assert.NoError(t, observer.QueryRowContext(t.Context(),
						"SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", name).Scan(&exists))
					assert.False(t, exists, "failed database must be dropped before the next attempt")
					assert.NotEqual(t, name, config.Name)
				}
				statement := "CREATE TABLE fixture_ready (id integer)"
				if attempts <= test.failures {
					message := test.message
					if message == "" {
						message = "fixture failure"
					}
					// Use the real PostgreSQL driver and migrator so the retry sees
					// the same wrapped error and dirty marker as a CI deadlock.
					statement = fmt.Sprintf("-- Mentioning SQLSTATE 40P01 or tuple concurrently deleted in SQL must not make a permanent error retryable.\nDO $$ BEGIN RAISE EXCEPTION USING ERRCODE = '%s', MESSAGE = '%s'; END $$;", test.code, message)
				}
				files := fstest.MapFS{"000001_fixture.up.sql": {Data: []byte(statement)}}
				migrationErr := migrateTestDBSourceTo(t.Context(), conn, config.Name, files, 1)
				assert.Equal(t, 0, conn.Stats().InUse, "migration must release its pinned connection")
				if migrationErr != nil {
					var dirty bool
					assert.NoError(t, conn.QueryRowContext(t.Context(), "SELECT dirty FROM schema_migrations").Scan(&dirty))
					assert.True(t, dirty)
					failedNames = append(failedNames, config.Name)
					failedConnections = append(failedConnections, conn)
				}
				return migrationErr
			})
			assert.Equal(t, test.wantAttempts, attempts)
			if test.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "SQLSTATE "+test.code)
				assert.True(t, conn == nil && config == nil)
			} else {
				assert.NoError(t, err)
				var version int
				var dirty bool
				assert.NoError(t, conn.QueryRowContext(t.Context(), "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
				assert.Equal(t, 1, version)
				assert.False(t, dirty)
				_, err = conn.ExecContext(t.Context(), "INSERT INTO fixture_ready VALUES (1)")
				assert.NoError(t, err)
			}
			for i, name := range failedNames {
				var exists bool
				assert.NoError(t, observer.QueryRowContext(t.Context(),
					"SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", name).Scan(&exists))
				assert.False(t, exists, "failed database must be dropped even on terminal errors")
				assert.Error(t, failedConnections[i].PingContext(t.Context()))
			}
		})
	}
}
