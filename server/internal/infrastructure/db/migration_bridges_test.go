package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/block/proto-fleet/server/migrations"
)

func TestCurtailmentAuthorizationEnvelopeBridgePreservesLegacyRows(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	for _, test := range []struct {
		name         string
		startVersion uint
		dirty        bool
	}{
		{name: "v0.2.9 version 130 upgrade", startVersion: 130},
		{name: "clean version 142 upgrade", startVersion: 142},
		{name: "RC1 dirty version 143 recovery", startVersion: 142, dirty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn, config := newMigrationBridgeTestDB(t)
			migrateTestDBTo(t, conn, config.Name, test.startVersion)
			insertLegacyCurtailmentRows(t, conn)

			if test.dirty {
				_, err := conn.ExecContext(t.Context(),
					"UPDATE schema_migrations SET version = 143, dirty = true")
				assert.NoError(t, err)
			}

			assert.NoError(t, runMigrationsWithCompatibilityBridges(conn, config))

			var version int
			var dirty bool
			assert.NoError(t, conn.QueryRowContext(t.Context(),
				"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
			assert.Equal(t, 143, version)
			assert.False(t, dirty)

			assertLegacyCurtailmentEnvelope(t, conn,
				"curtailment_response_profile", true)
			assertLegacyCurtailmentEnvelope(t, conn,
				"curtailment_event", true)

			var profiles, events int
			assert.NoError(t, conn.QueryRowContext(t.Context(), `
				SELECT
					(SELECT count(*) FROM curtailment_response_profile),
					(SELECT count(*) FROM curtailment_event)
			`).Scan(&profiles, &events))
			assert.Equal(t, 1, profiles)
			assert.Equal(t, 9, events)

		})
	}
}

func TestCurtailmentAuthorizationEnvelopeBridgeAllowsFreshBootstrap(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	assert.NoError(t, runMigrationsWithCompatibilityBridges(conn, config))

	var version int
	var dirty bool
	assert.NoError(t, conn.QueryRowContext(t.Context(),
		"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 143, version)
	assert.False(t, dirty)
}

func TestCurtailmentAuthorizationEnvelopeBridgeUsesMigrationLock(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	migrateTestDBTo(t, conn, config.Name, 142)
	insertLegacyCurtailmentRows(t, conn)

	_, blocker, err := newMigrator(conn, config)
	assert.NoError(t, err)
	assert.NoError(t, blocker.Lock())
	locked := true
	defer func() {
		if locked {
			assert.NoError(t, blocker.Unlock())
		}
	}()

	concurrentConn, err := ConnectToDatabase(config)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, concurrentConn.Close()) })
	assert.NoError(t, concurrentConn.PingContext(t.Context()))

	done := make(chan error, 1)
	go func() {
		done <- runMigrationsWithCompatibilityBridges(concurrentConn, config)
	}()

	select {
	case err := <-done:
		t.Fatalf("migration bridge completed before advisory lock was released: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	assert.NoError(t, blocker.Unlock())
	locked = false

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("migration bridge did not complete after advisory lock was released")
	}

	var version int
	var dirty bool
	assert.NoError(t, conn.QueryRowContext(t.Context(),
		"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 143, version)
	assert.False(t, dirty)
}

func TestCurtailmentAuthorizationEnvelopeBridgeResetsEmptyDirtyRC1(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	migrateTestDBTo(t, conn, config.Name, 142)
	_, err := conn.ExecContext(t.Context(),
		"UPDATE schema_migrations SET version = 143, dirty = true")
	assert.NoError(t, err)

	assert.NoError(t, runMigrationsWithCompatibilityBridges(conn, config))

	var version int
	var dirty bool
	assert.NoError(t, conn.QueryRowContext(t.Context(),
		"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 143, version)
	assert.False(t, dirty)
}

func newMigrationBridgeTestDB(t *testing.T) (*sql.DB, *Config) {
	t.Helper()

	cli := struct {
		DB Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	assert.NoError(t, err)
	_, err = parser.Parse(nil)
	assert.NoError(t, err)

	adminConfig := cli.DB
	adminConfig.Name = "postgres"
	admin, err := ConnectToDatabase(&adminConfig)
	assert.NoError(t, err)
	defer admin.Close()

	suffix := make([]byte, 6)
	_, err = rand.Read(suffix)
	assert.NoError(t, err)
	dbName := "fleet_bridge_" + hex.EncodeToString(suffix)

	// nolint:forbidigo // Test-only database lifecycle DDL cannot be parameterized.
	_, err = admin.ExecContext(t.Context(), "CREATE DATABASE "+quoteTestIdentifier(dbName))
	assert.NoError(t, err)
	t.Cleanup(func() {
		// nolint: usetesting
		ctx := context.Background()
		cleanup, cleanupErr := ConnectToDatabase(&adminConfig)
		assert.NoError(t, cleanupErr)
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(ctx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		// nolint:forbidigo // Test-only database lifecycle DDL cannot be parameterized.
		_, cleanupErr = cleanup.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteTestIdentifier(dbName))
		assert.NoError(t, cleanupErr)
	})

	config := cli.DB
	config.Name = dbName
	conn, err := ConnectToDatabase(&config)
	assert.NoError(t, err)
	assert.NoError(t, conn.PingContext(t.Context()))
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	return conn, &config
}

func migrateTestDBTo(t *testing.T, conn *sql.DB, databaseName string, version uint) {
	t.Helper()

	source, err := iofs.New(migrations.Migrations, ".")
	assert.NoError(t, err)
	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	assert.NoError(t, err)
	migration, err := migrate.NewWithInstance("migrations", source, databaseName, driver)
	assert.NoError(t, err)
	// Do not call migration.Close: postgres.WithInstance would close conn, which
	// the caller continues using. Test cleanup closes conn and its driver resources.
	assert.NoError(t, migration.Migrate(version))
}

func insertLegacyCurtailmentRows(t *testing.T, conn *sql.DB) {
	t.Helper()

	var orgID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name)
		VALUES ('bridge-org', 'Bridge org')
		RETURNING id
	`).Scan(&orgID))

	var userID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO "user" (user_id, username, password_hash)
		VALUES ('bridge-user', 'bridge-user', 'test-hash')
		RETURNING id
	`).Scan(&userID))

	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_response_profile (
			org_id, profile_name, mode, target_kw, scope_json
		) VALUES ($1, 'Legacy profile', 'FIXED_KW', 100, '{}'::jsonb)
	`, orgID)
	assert.NoError(t, err)

	for i := 1; i <= 9; i++ {
		_, err = conn.ExecContext(t.Context(), `
			INSERT INTO curtailment_event (
				event_uuid, org_id, state, mode, strategy, level, priority,
				loop_type, scope_type, scope_jsonb,
				restore_batch_size, restore_batch_interval_sec,
				source_actor_type, reason, created_by_user_id
			) VALUES (
				$1, $2,
				'completed', 'FULL_FLEET', 'LEAST_EFFICIENT_FIRST', 'FULL', 'NORMAL',
				'open', 'whole_org', '{}'::jsonb,
				50, 5, 'user', 'Legacy event', $3
			)
		`, fmt.Sprintf("00000000-0000-0000-0000-%012d", i), orgID, userID)
		assert.NoError(t, err)
	}
}

func assertLegacyCurtailmentEnvelope(
	t *testing.T,
	conn *sql.DB,
	table string,
	wantMinerScopeUnbounded bool,
) {
	t.Helper()

	if table != "curtailment_response_profile" && table != "curtailment_event" {
		t.Fatalf("unsupported envelope table %q", table)
	}
	var schemaVersion int
	var minerScopeUnbounded bool
	var selected, members string
	// nolint:gosec // table is checked against the two static migration-owned names above.
	query := fmt.Sprintf(`
		SELECT
			(authorization_envelope_jsonb->>'schema_version')::int,
			(authorization_envelope_jsonb->>'miner_scope_unbounded')::boolean,
			authorization_envelope_jsonb->'selected_resource_site_ids',
			authorization_envelope_jsonb->'current_member_site_ids'
		FROM %s
	`, table)
	err := conn.QueryRowContext(t.Context(), query).Scan(
		&schemaVersion, &minerScopeUnbounded, &selected, &members,
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, schemaVersion)
	assert.Equal(t, wantMinerScopeUnbounded, minerScopeUnbounded)
	assert.Equal(t, "[]", selected)
	assert.Equal(t, "[]", members)
}

func quoteTestIdentifier(name string) string {
	return `"` + name + `"`
}
