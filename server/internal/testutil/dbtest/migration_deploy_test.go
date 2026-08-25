package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/migrations"
)

func TestMigrationDeployPathsReach161(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database migration integration test in short mode")
	}

	tests := []struct {
		name       string
		startAt143 bool
	}{
		{name: "fresh empty database 0 to 161"},
		{name: "main 143 database to 161", startAt143: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newEmptyMigrationTestDatabase(t)
			migration := newDeployMigration(t, conn)

			if tt.startAt143 {
				if err := migration.Migrate(143); err != nil && !errors.Is(err, migrate.ErrNoChange) {
					t.Fatalf("migrate to main 143: %v", err)
				}
				assertMigrationVersion(t, migration, 143)
			}
			if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				t.Fatalf("migrate to 161: %v", err)
			}
			assertMigrationVersion(t, migration, 161)

			var schemaReady bool
			if err := conn.QueryRowContext(t.Context(), `
				SELECT to_regclass('rollout_lane_model') IS NOT NULL
				   AND to_regclass('firmware_rollout_group') IS NOT NULL
				   AND to_regclass('firmware_rollout_evidence_accumulator') IS NOT NULL
				   AND EXISTS (
				       SELECT 1
				       FROM information_schema.columns
				       WHERE table_schema = current_schema()
				         AND table_name = 'firmware_rollout_batch'
				         AND column_name = 'evidence_cancellation_reason'
				   )
			`).Scan(&schemaReady); err != nil {
				t.Fatalf("inspect migrated schema: %v", err)
			}
			if !schemaReady {
				t.Fatal("migration path did not produce the complete version 161 schema")
			}
		})
	}
}

func TestMigration161RollsBackAndReappliesCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database migration integration test in short mode")
	}

	conn := newEmptyMigrationTestDatabase(t)
	migration := newDeployMigration(t, conn)
	require.NoError(t, migration.Up())
	assertMigrationVersion(t, migration, 161)

	require.NoError(t, migration.Steps(-1))
	assertMigrationVersion(t, migration, 160)
	var accumulatorExists bool
	require.NoError(t, conn.QueryRowContext(
		t.Context(),
		"SELECT to_regclass('firmware_rollout_evidence_accumulator') IS NOT NULL",
	).Scan(&accumulatorExists))
	require.False(t, accumulatorExists)

	require.NoError(t, migration.Steps(1))
	assertMigrationVersion(t, migration, 161)
	require.NoError(t, conn.QueryRowContext(
		t.Context(),
		"SELECT to_regclass('firmware_rollout_evidence_accumulator') IS NOT NULL",
	).Scan(&accumulatorExists))
	require.True(t, accumulatorExists)
}

func newEmptyMigrationTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	cli := struct {
		DB db.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("parse database config: %v", err)
	}
	if _, err = parser.Parse(nil); err != nil {
		t.Fatalf("load database config: %v", err)
	}

	adminConfig := cli.DB
	adminConfig.Name = "postgres"
	name := generateTestDBName(t.Name())
	createTestDatabase(t, &adminConfig, name, "")

	testConfig := cli.DB
	testConfig.Name = name
	conn, err := db.ConnectToDatabase(&testConfig)
	if err != nil {
		t.Fatalf("connect to empty migration database: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		// nolint: usetesting // cleanup runs after t.Context is cancelled.
		dropTestDatabase(context.Background(), t, &adminConfig, name)
	})
	return conn
}

func newDeployMigration(t *testing.T, conn *sql.DB) *migrate.Migrate {
	t.Helper()
	source, err := iofs.New(migrations.Migrations, ".")
	if err != nil {
		t.Fatalf("create migration source: %v", err)
	}
	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		t.Fatalf("create migration database driver: %v", err)
	}
	instance, err := migrate.NewWithInstance("deploy-migrations", source, "deploy-migrations", driver)
	if err != nil {
		t.Fatalf("create migration instance: %v", err)
	}
	t.Cleanup(func() {
		_, _ = instance.Close()
	})
	return instance
}

func assertMigrationVersion(t *testing.T, migration *migrate.Migrate, expected uint) {
	t.Helper()
	version, dirty, err := migration.Version()
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != expected || dirty {
		t.Fatalf("migration version = %d dirty=%t, want %d clean", version, dirty, expected)
	}
}
