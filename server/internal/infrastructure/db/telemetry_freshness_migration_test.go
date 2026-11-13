package db

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestTelemetryFreshnessMigrationRespectsCheckpointTransaction(t *testing.T) {
	requireBaselineDB(t)
	conn, _ := newMigrationBridgeTestDB(t)
	before := fstest.MapFS{}
	var up, down []byte
	found := false
	entries, err := fs.ReadDir(migrations.Current, "current")
	require.NoError(t, err)
	for _, entry := range entries {
		path := "current/" + entry.Name()
		body, err := fs.ReadFile(migrations.Current, path)
		require.NoError(t, err)
		if strings.Contains(path, "_internal_telemetry_freshness.") {
			if strings.HasSuffix(path, ".up.sql") {
				up = body
			} else {
				down = body
			}
			found = true
			continue
		}
		// Later migrations are applied with the full set below.
		if found {
			continue
		}
		before[path] = &fstest.MapFile{Data: body}
	}
	require.NotEmpty(t, up)
	require.NotEmpty(t, down)
	require.NoError(t, runCurrentMigrations(t.Context(), conn, before))
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	var exists bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('telemetry_freshness') IS NOT NULL`).Scan(&exists))
	require.False(t, exists, "up SQL must not commit ahead of the checkpoint")
	require.NoError(t, runCurrentMigrations(t.Context(), conn, migrations.Current))
	tx, err = conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('telemetry_freshness') IS NOT NULL`).Scan(&exists))
	require.True(t, exists, "down SQL must not commit ahead of the checkpoint")
}
