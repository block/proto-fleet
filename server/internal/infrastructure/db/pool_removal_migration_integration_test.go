package db

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestPoolRemovalPublicAndInternalUpgrade(t *testing.T) {
	requireBaselineDB(t)
	for _, internal := range []bool{false, true} {
		name := "public schema"
		if internal {
			name = "internal schema"
		}
		t.Run(name, func(t *testing.T) {
			conn, _ := newMigrationBridgeTestDB(t)
			previous, current := fstest.MapFS{}, fstest.MapFS{}
			paths, err := fs.Glob(migrations.Current, "current/*.sql")
			require.NoError(t, err)
			for _, path := range paths {
				// This fixture tests the historical split schemas. Later migrations
				// require the consolidated schema, covered by TestConsolidationUpgradesPublicBaseline.
				if path >= "current/"+migrations.ConsolidationName {
					continue
				}
				if !internal && strings.Contains(path, "_internal_") {
					continue
				}
				body, err := fs.ReadFile(migrations.Current, path)
				require.NoError(t, err)
				current[path] = &fstest.MapFile{Data: body}
				if path < "current/001067" {
					previous[path] = current[path]
				}
			}
			require.NoError(t, runCurrentMigrations(t.Context(), conn, previous))
			require.NoError(t, runCurrentMigrations(t.Context(), conn, current))
			require.NoError(t, runCurrentMigrations(t.Context(), conn, current), "repeated startup must be safe")
			var table, hosted bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('pool_configuration_removal') IS NOT NULL, to_regclass('hosted_account') IS NOT NULL`).Scan(&table, &hosted))
			require.False(t, table, "removal must reuse shared operation storage")
			require.Equal(t, internal, hosted)
			var version int
			var dirty bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty))
			require.False(t, dirty)
			_, latest, err := migrationCheckpoints(current)
			require.NoError(t, err)
			require.Equal(t, latest, version)
			var defaultKind string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT column_default FROM information_schema.columns WHERE table_name='pool_configuration_operation' AND column_name='kind'`).Scan(&defaultKind))
			require.Contains(t, defaultKind, "apply")
			var constraint string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='pool_configuration_operation'::regclass AND conname='pool_configuration_operation_kind_check'`).Scan(&constraint))
			require.Contains(t, constraint, "clear")
			require.Contains(t, constraint, "unassign")

		})
	}
}
