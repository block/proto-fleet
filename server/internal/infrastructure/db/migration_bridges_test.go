package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"

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
			assert.Equal(t, 145, version)
			assert.False(t, dirty)

			assertLegacyCurtailmentEnvelope(t, conn,
				"curtailment_response_profile", true)
			assertLegacyCurtailmentEnvelope(t, conn,
				"curtailment_event", true)

			var profiles, rules, events int
			assert.NoError(t, conn.QueryRowContext(t.Context(), `
				SELECT
					(SELECT count(*) FROM curtailment_response_profile),
					(SELECT count(*) FROM curtailment_automation_rule),
					(SELECT count(*) FROM curtailment_event)
			`).Scan(&profiles, &rules, &events))
			assert.Equal(t, 1, profiles)
			assert.Equal(t, 3, rules)
			assert.Equal(t, 12, events)
			assertLegacyResponseProfileRevisionBinding(t, conn)
		})
	}
}

func TestResponseProfileRevisionMigrationSupportsPassiveFirstHAWrites(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	migrateTestDBTo(t, conn, config.Name, 142)
	insertLegacyCurtailmentRows(t, conn)
	assert.NoError(t, runMigrationCompatibilityBridges(t.Context(), conn))

	profileColumnsBefore := migrationTableColumns(t, conn, "curtailment_response_profile")
	ruleColumnsBefore := migrationTableColumns(t, conn, "curtailment_automation_rule")
	migrateTestDBTo(t, conn, config.Name, 144)
	assert.Equal(t, profileColumnsBefore, migrationTableColumns(t, conn, "curtailment_response_profile"))
	assert.Equal(t, ruleColumnsBefore, migrationTableColumns(t, conn, "curtailment_automation_rule"))

	var orgID, sourceID, originalProfileID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT profile.org_id, rule.mqtt_source_id, profile.id
		FROM curtailment_response_profile AS profile
		JOIN curtailment_automation_rule AS rule
		  ON rule.response_profile_id = profile.id
		WHERE rule.rule_name = 'Legacy rule'
	`).Scan(&orgID, &sourceID, &originalProfileID))

	// These statements intentionally use the pre-migration column contract.
	// The new passive binary has migrated the shared database, but the previous
	// active binary may still issue them until takeover completes.
	var profileID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_response_profile (
			org_id, profile_name, mode, target_kw, tolerance_kw,
			scope_json, authorization_envelope_jsonb
		) VALUES (
			$1, 'Rolling update profile', 'FIXED_KW', 100, NULL,
			'{}'::JSONB,
			'{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":true,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}'::JSONB
		)
		RETURNING id
	`, orgID).Scan(&profileID))

	var initialRevision uuid.UUID
	var wholeOrg bool
	var scopeVersion int
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT
			profile_revision.revision,
			(profile.scope_json->>'whole_org')::BOOLEAN,
			(profile.scope_json->>'scope_schema_version')::INT
		FROM curtailment_response_profile AS profile
		JOIN curtailment_response_profile_revision AS profile_revision
		  ON profile_revision.response_profile_id = profile.id
		WHERE profile.id = $1
	`, profileID).Scan(&initialRevision, &wholeOrg, &scopeVersion))
	assert.NotEqual(t, uuid.Nil, initialRevision)
	assert.True(t, wholeOrg)
	assert.Equal(t, 1, scopeVersion)

	var ruleID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_automation_rule (
			org_id, rule_name, mqtt_source_id, response_profile_id
		) VALUES ($1, 'Rolling update rule', $2, $3)
		RETURNING id
	`, orgID, sourceID, profileID).Scan(&ruleID))

	var boundRevision uuid.UUID
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT response_profile_revision
		FROM curtailment_automation_rule_profile_revision
		WHERE automation_rule_id = $1
	`, ruleID).Scan(&boundRevision))
	assert.Equal(t, initialRevision, boundRevision)

	var userID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT id FROM "user" WHERE user_id = 'bridge-user'
	`).Scan(&userID))
	ruleReference := strconv.FormatInt(ruleID, 10)
	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority,
			loop_type, scope_type, scope_jsonb, mode_params_jsonb,
			curtail_batch_size, curtail_batch_interval_sec,
			restore_batch_size, restore_batch_interval_sec,
			include_maintenance, force_include_maintenance,
			force_include_all_paired_miners,
			facility_fan_device_ids, fan_off_delay_sec, fan_restore_delay_sec,
			decision_snapshot_jsonb, authorization_envelope_jsonb,
			source_actor_type, source_actor_id,
			external_source, external_reference, idempotency_key,
			reason, created_by_user_id
		)
		SELECT
			'00000000-0000-0000-0000-000000000020', $1, 'active',
			profile.mode, profile.strategy, profile.level, profile.priority,
			'open', 'whole_org', '{}'::JSONB,
			jsonb_build_object(
				'target_kw', profile.target_kw,
				'tolerance_kw', COALESCE(profile.tolerance_kw, 0)
			),
			profile.curtail_batch_size, profile.curtail_batch_interval_sec,
			profile.restore_batch_size, profile.restore_batch_interval_sec,
			profile.include_maintenance, profile.force_include_maintenance,
			profile.force_include_all_paired_miners,
			profile.facility_fan_device_ids, profile.fan_off_delay_sec,
			profile.fan_restore_delay_sec,
			'{"selected_count":1}'::JSONB, profile.authorization_envelope_jsonb,
			'automation', $2, 'curtailment_automation', $2,
			'curtailment_automation_rule:' || $2,
			'Rolling update automation event', $3
		FROM curtailment_response_profile AS profile
		WHERE profile.id = $4
	`, orgID, ruleReference, userID, profileID)
	assert.NoError(t, err)
	var eventRevision uuid.UUID
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT (decision_snapshot_jsonb->>'response_profile_revision')::UUID
		FROM curtailment_event
		WHERE event_uuid = '00000000-0000-0000-0000-000000000020'
	`).Scan(&eventRevision))
	assert.Equal(t, initialRevision, eventRevision)

	_, err = conn.ExecContext(t.Context(), `
		UPDATE curtailment_response_profile
		SET tolerance_kw = 0
		WHERE id = $1
	`, profileID)
	assert.NoError(t, err)
	var effectiveZeroRevision uuid.UUID
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT revision
		FROM curtailment_response_profile_revision
		WHERE response_profile_id = $1
	`, profileID).Scan(&effectiveZeroRevision))
	assert.Equal(t, initialRevision, effectiveZeroRevision)

	_, err = conn.ExecContext(t.Context(), `
		UPDATE curtailment_response_profile
		SET restore_batch_size = restore_batch_size + 1
		WHERE id = $1
	`, profileID)
	assert.NoError(t, err)
	var rotatedRevision uuid.UUID
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT revision
		FROM curtailment_response_profile_revision
		WHERE response_profile_id = $1
	`, profileID).Scan(&rotatedRevision))
	assert.NotEqual(t, initialRevision, rotatedRevision)

	_, err = conn.ExecContext(t.Context(), `
		UPDATE curtailment_automation_rule
		SET response_profile_id = $2
		WHERE id = $1
	`, ruleID, originalProfileID)
	assert.NoError(t, err)
	var reboundRevision, originalProfileRevision uuid.UUID
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT
			rule_revision.response_profile_revision,
			profile_revision.revision
		FROM curtailment_automation_rule_profile_revision AS rule_revision
		JOIN curtailment_automation_rule AS rule
		  ON rule.id = rule_revision.automation_rule_id
		JOIN curtailment_response_profile_revision AS profile_revision
		  ON profile_revision.response_profile_id = rule.response_profile_id
		WHERE rule.id = $1
	`, ruleID).Scan(&reboundRevision, &originalProfileRevision))
	assert.Equal(t, originalProfileRevision, reboundRevision)
}

func TestResponseProfileRevisionDownMigrationPreservesTerminalProvenance(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	migrateTestDBTo(t, conn, config.Name, 142)
	insertLegacyCurtailmentRows(t, conn)
	assert.NoError(t, runMigrationCompatibilityBridges(t.Context(), conn))
	migrateTestDBTo(t, conn, config.Name, 144)

	_, err := conn.ExecContext(t.Context(), `
		UPDATE curtailment_event
		SET source_actor_type = 'automation',
		    external_source = 'curtailment_automation',
		    decision_snapshot_jsonb = decision_snapshot_jsonb || jsonb_build_object(
		        'response_profile_id', 123,
		        'response_profile_revision', '11111111-1111-4111-8111-111111111111'
		    )
		WHERE event_uuid = '00000000-0000-0000-0000-000000000001'
	`)
	assert.NoError(t, err)

	migrateTestDBTo(t, conn, config.Name, 143)

	var liveHasBinding, terminalHasBinding bool
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT decision_snapshot_jsonb ? 'response_profile_id'
		    OR decision_snapshot_jsonb ? 'response_profile_revision'
		FROM curtailment_event
		WHERE event_uuid = '00000000-0000-0000-0000-000000000010'
	`).Scan(&liveHasBinding))
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT decision_snapshot_jsonb ? 'response_profile_id'
		    AND decision_snapshot_jsonb ? 'response_profile_revision'
		FROM curtailment_event
		WHERE event_uuid = '00000000-0000-0000-0000-000000000001'
	`).Scan(&terminalHasBinding))
	assert.False(t, liveHasBinding)
	assert.True(t, terminalHasBinding)
}

func TestCurtailmentAuthorizationEnvelopeBridgeUsesActiveSchema(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	const schema = "migration_bridge_schema"
	_, err := conn.ExecContext(t.Context(), "CREATE EXTENSION IF NOT EXISTS timescaledb WITH SCHEMA public")
	assert.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA public")
	assert.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "CREATE SCHEMA "+schema)
	assert.NoError(t, err)

	searchPathConfig := *config
	searchPathConfig.ExplicitDSN = config.DSN() + "&search_path=" + schema + ",public"
	searchPathConn, err := ConnectToDatabase(&searchPathConfig)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, searchPathConn.Close()) })
	assert.NoError(t, searchPathConn.PingContext(t.Context()))

	migrateTestDBTo(t, searchPathConn, config.Name, 142)
	// Simulate a newer public Fleet schema. The bridge must inspect the schema
	// owning the active schema_migrations relation, not these public columns.
	_, err = conn.ExecContext(t.Context(), `
		CREATE TABLE public.curtailment_response_profile (
			authorization_envelope_jsonb jsonb NOT NULL
		);
		CREATE TABLE public.curtailment_event (
			authorization_envelope_jsonb jsonb NOT NULL
		)
	`)
	assert.NoError(t, err)
	insertLegacyCurtailmentRows(t, searchPathConn)
	assert.NoError(t, runMigrationsWithCompatibilityBridges(searchPathConn, &searchPathConfig))

	var version int
	var dirty bool
	assert.NoError(t, searchPathConn.QueryRowContext(t.Context(),
		"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 145, version)
	assert.False(t, dirty)
	assertLegacyCurtailmentEnvelope(t, searchPathConn, "curtailment_response_profile", true)
	assertLegacyResponseProfileRevisionBinding(t, searchPathConn)
}

func TestMigrationsThroughVersionNeverDowngrade(t *testing.T) {
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for migration integration tests")
	}

	conn, config := newMigrationBridgeTestDB(t)
	assert.NoError(t, runMigrationsWithCompatibilityBridges(conn, config))

	_, driver, err := newMigrator(conn, config)
	assert.NoError(t, err)
	through142, err := newMigratorThrough(config.Name, driver, 142)
	assert.NoError(t, err)
	assert.NoError(t, runMigrationsThroughVersion(through142, 142))

	var version int
	var dirty bool
	assert.NoError(t, conn.QueryRowContext(t.Context(),
		"SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 145, version)
	assert.False(t, dirty)
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
	assert.Equal(t, 145, version)
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
	assert.Equal(t, 145, version)
	assert.False(t, dirty)
	assertLegacyResponseProfileRevisionBinding(t, conn)
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
	assert.Equal(t, 145, version)
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

func migrationTableColumns(t *testing.T, conn *sql.DB, table string) []string {
	t.Helper()
	if table != "curtailment_response_profile" && table != "curtailment_automation_rule" {
		t.Fatalf("unsupported migration table %q", table)
	}
	rows, err := conn.QueryContext(t.Context(), `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = $1
		ORDER BY ordinal_position
	`, table)
	assert.NoError(t, err)
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		assert.NoError(t, rows.Scan(&column))
		columns = append(columns, column)
	}
	assert.NoError(t, rows.Err())
	return columns
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

	var siteID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO site (org_id, name, slug)
		VALUES ($1, 'Bridge site', 'bridge-site')
		RETURNING id
	`, orgID).Scan(&siteID))

	var profileID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_response_profile (
			org_id, profile_name, mode, target_kw, site_id, scope_json
		) VALUES (
			$1, 'Legacy profile', 'FIXED_KW', 100, $2, '{}'::JSONB
		)
		RETURNING id
	`, orgID, siteID).Scan(&profileID))

	var sourceID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_mqtt_source_config (
			organization_id, service_user_id, source_name, topic,
			broker_primary_host, broker_secondary_host,
			mqtt_username, mqtt_password_enc
		) VALUES (
			$1, $2, 'Bridge source', 'bridge/target',
			'primary.example', 'secondary.example',
			'bridge-user', 'encrypted-password'
		)
		RETURNING id
	`, orgID, userID).Scan(&sourceID))

	var ruleID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_automation_rule (
			org_id, rule_name, mqtt_source_id, response_profile_id
		) VALUES ($1, 'Legacy rule', $2, $3)
		RETURNING id
	`, orgID, sourceID, profileID).Scan(&ruleID))

	ruleReference := strconv.FormatInt(ruleID, 10)
	_, err := conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority,
			loop_type, scope_type, scope_jsonb, mode_params_jsonb,
			curtail_batch_size, curtail_batch_interval_sec,
			restore_batch_size, restore_batch_interval_sec,
			include_maintenance, force_include_maintenance,
			force_include_all_paired_miners,
			facility_fan_device_ids, fan_off_delay_sec, fan_restore_delay_sec,
			decision_snapshot_jsonb, source_actor_type, source_actor_id,
			external_source, external_reference, idempotency_key,
			reason, created_by_user_id
		)
		SELECT
			'00000000-0000-0000-0000-000000000010', $1, 'active',
			profile.mode, profile.strategy, profile.level, profile.priority,
			'open', 'site', jsonb_build_object('site_id', $4::BIGINT),
			jsonb_build_object(
				'target_kw', profile.target_kw,
				'tolerance_kw', COALESCE(profile.tolerance_kw, 0)
			),
			profile.curtail_batch_size, profile.curtail_batch_interval_sec,
			profile.restore_batch_size, profile.restore_batch_interval_sec,
			profile.include_maintenance, profile.force_include_maintenance,
			profile.force_include_all_paired_miners,
			profile.facility_fan_device_ids, profile.fan_off_delay_sec,
			profile.fan_restore_delay_sec,
			'{"selected_count":1}'::jsonb,
			'automation', $2, 'curtailment_automation', $2,
			'curtailment_automation_rule:' || $2,
			'Legacy automation event', $3
		FROM curtailment_response_profile AS profile
		WHERE profile.id = $5
	`, orgID, ruleReference, userID, siteID, profileID)
	assert.NoError(t, err)

	var mismatchedRuleID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_automation_rule (
			org_id, rule_name, mqtt_source_id, response_profile_id
		) VALUES ($1, 'Mismatched legacy rule', $2, $3)
		RETURNING id
	`, orgID, sourceID, profileID).Scan(&mismatchedRuleID))
	mismatchedRuleReference := strconv.FormatInt(mismatchedRuleID, 10)
	_, err = conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority,
			loop_type, scope_type, scope_jsonb, mode_params_jsonb,
			curtail_batch_size, curtail_batch_interval_sec,
			restore_batch_size, restore_batch_interval_sec,
			include_maintenance, force_include_maintenance,
			force_include_all_paired_miners,
			facility_fan_device_ids, fan_off_delay_sec, fan_restore_delay_sec,
			decision_snapshot_jsonb, source_actor_type, source_actor_id,
			external_source, external_reference, idempotency_key,
			reason, created_by_user_id
		)
		SELECT
			'00000000-0000-0000-0000-000000000011', $1, 'active',
			profile.mode, profile.strategy, profile.level, 'EMERGENCY',
			'open', 'site', jsonb_build_object('site_id', $4::BIGINT),
			jsonb_build_object(
				'target_kw', profile.target_kw,
				'tolerance_kw', COALESCE(profile.tolerance_kw, 0)
			),
			profile.curtail_batch_size, profile.curtail_batch_interval_sec,
			profile.restore_batch_size, profile.restore_batch_interval_sec,
			profile.include_maintenance, profile.force_include_maintenance,
			profile.force_include_all_paired_miners,
			profile.facility_fan_device_ids, profile.fan_off_delay_sec,
			profile.fan_restore_delay_sec,
			'{"selected_count":1}'::jsonb,
			'automation', $2, 'curtailment_automation', $2,
			'curtailment_automation_rule:' || $2,
			'Mismatched legacy automation event', $3
		FROM curtailment_response_profile AS profile
		WHERE profile.id = $5
	`, orgID, mismatchedRuleReference, userID, siteID, profileID)
	assert.NoError(t, err)

	var mismatchedScopeRuleID int64
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_automation_rule (
			org_id, rule_name, mqtt_source_id, response_profile_id
		) VALUES ($1, 'Mismatched scope legacy rule', $2, $3)
		RETURNING id
	`, orgID, sourceID, profileID).Scan(&mismatchedScopeRuleID))
	mismatchedScopeRuleReference := strconv.FormatInt(mismatchedScopeRuleID, 10)
	_, err = conn.ExecContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority,
			loop_type, scope_type, scope_jsonb, mode_params_jsonb,
			curtail_batch_size, curtail_batch_interval_sec,
			restore_batch_size, restore_batch_interval_sec,
			include_maintenance, force_include_maintenance,
			force_include_all_paired_miners,
			facility_fan_device_ids, fan_off_delay_sec, fan_restore_delay_sec,
			decision_snapshot_jsonb, source_actor_type, source_actor_id,
			external_source, external_reference, idempotency_key,
			reason, created_by_user_id
		)
		SELECT
			'00000000-0000-0000-0000-000000000012', $1, 'active',
			profile.mode, profile.strategy, profile.level, profile.priority,
			'open', 'site', jsonb_build_object('site_id', $4::BIGINT + 1),
			jsonb_build_object(
				'target_kw', profile.target_kw,
				'tolerance_kw', COALESCE(profile.tolerance_kw, 0)
			),
			profile.curtail_batch_size, profile.curtail_batch_interval_sec,
			profile.restore_batch_size, profile.restore_batch_interval_sec,
			profile.include_maintenance, profile.force_include_maintenance,
			profile.force_include_all_paired_miners,
			profile.facility_fan_device_ids, profile.fan_off_delay_sec,
			profile.fan_restore_delay_sec,
			'{"selected_count":1}'::jsonb,
			'automation', $2, 'curtailment_automation', $2,
			'curtailment_automation_rule:' || $2,
			'Mismatched scope legacy automation event', $3
		FROM curtailment_response_profile AS profile
		WHERE profile.id = $5
	`, orgID, mismatchedScopeRuleReference, userID, siteID, profileID)
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

func assertLegacyResponseProfileRevisionBinding(t *testing.T, conn *sql.DB) {
	t.Helper()

	var profileRevision, ruleRevision uuid.UUID
	var profileSiteID, scopeSiteID int64
	var scopeSchemaVersion int
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT
			profile_revision.revision,
			rule_revision.response_profile_revision,
			profile.site_id,
			(profile.scope_json->'site_ids'->>0)::BIGINT,
			(profile.scope_json->>'scope_schema_version')::INT
		FROM curtailment_response_profile AS profile
		JOIN curtailment_response_profile_revision AS profile_revision
		  ON profile_revision.response_profile_id = profile.id
		JOIN curtailment_automation_rule AS rule
		  ON rule.response_profile_id = profile.id
		 AND rule.org_id = profile.org_id
		JOIN curtailment_automation_rule_profile_revision AS rule_revision
		  ON rule_revision.automation_rule_id = rule.id
		WHERE rule.rule_name = 'Legacy rule'
	`).Scan(&profileRevision, &ruleRevision, &profileSiteID, &scopeSiteID, &scopeSchemaVersion))
	assert.NotEqual(t, uuid.Nil, profileRevision)
	assert.Equal(t, profileRevision, ruleRevision)
	assert.Equal(t, profileSiteID, scopeSiteID)
	assert.Equal(t, 1, scopeSchemaVersion)

	var snapshotProfileID, ruleProfileID int64
	var snapshotRevision uuid.UUID
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT
			(event.decision_snapshot_jsonb->>'response_profile_id')::BIGINT,
			(event.decision_snapshot_jsonb->>'response_profile_revision')::UUID,
			rule.response_profile_id
		FROM curtailment_event AS event
		JOIN curtailment_automation_rule AS rule
		  ON event.org_id = rule.org_id
		 AND event.external_reference = rule.id::TEXT
		WHERE event.source_actor_type = 'automation'
		  AND event.external_source = 'curtailment_automation'
		  AND event.event_uuid = '00000000-0000-0000-0000-000000000010'
	`).Scan(&snapshotProfileID, &snapshotRevision, &ruleProfileID))
	assert.Equal(t, ruleProfileID, snapshotProfileID)
	assert.Equal(t, ruleRevision, snapshotRevision)

	var mismatchedEventStamped bool
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT decision_snapshot_jsonb ? 'response_profile_id'
		    OR decision_snapshot_jsonb ? 'response_profile_revision'
		FROM curtailment_event
		WHERE event_uuid = '00000000-0000-0000-0000-000000000011'
	`).Scan(&mismatchedEventStamped))
	assert.False(t, mismatchedEventStamped)

	var mismatchedScopeEventStamped bool
	assert.NoError(t, conn.QueryRowContext(t.Context(), `
		SELECT decision_snapshot_jsonb ? 'response_profile_id'
		    OR decision_snapshot_jsonb ? 'response_profile_revision'
		FROM curtailment_event
		WHERE event_uuid = '00000000-0000-0000-0000-000000000012'
	`).Scan(&mismatchedScopeEventStamped))
	assert.False(t, mismatchedScopeEventStamped)
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
