package db

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestTopologyCollisionUpgradePreservesDeviceWriteHistory(t *testing.T) {
	requireBaselineDB(t)
	for _, checkpoint := range []uint{1056, 1057} {
		t.Run(fmt.Sprint(checkpoint), func(t *testing.T) {
			conn, config := newMigrationBridgeTestDB(t)
			files, err := fs.Sub(migrations.Current, "current")
			require.NoError(t, err)
			require.NoError(t, migrateTestDBSourceTo(t.Context(), conn, config.Name, files, checkpoint))
			seedBaselineIdentity(t, conn)
			_, err = conn.ExecContext(t.Context(), `
INSERT INTO device_write_fence(device_id,organization_id) VALUES (999999,999999);
INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,owner_instance_id,identity_snapshot)
VALUES ('00000000-0000-0000-0000-000000000001',999999,999999,0,
 '00000000-0000-0000-0000-000000000002','{}');`)
			require.NoError(t, err)
			outcome := "{}"
			if checkpoint == 1057 {
				outcome = `{"execution": "not_started"}`
				_, err = conn.ExecContext(t.Context(), `UPDATE device_write_attempt SET write_outcome=$1`, outcome)
				require.NoError(t, err)
			}

			require.NoError(t, runCurrentMigrations(t.Context(), conn, migrations.Current))
			require.NoError(t, runCurrentMigrations(t.Context(), conn, migrations.Current), "repeated startup must not replay either migration")
			var version int
			var dirty bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty))
			_, latest, err := migrationCheckpoints(migrations.Current)
			require.NoError(t, err)
			require.Equal(t, latest, version)
			require.GreaterOrEqual(t, version, 1058)
			require.False(t, dirty)
			var stored, status string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT write_outcome::text,status FROM device_write_attempt`).Scan(&stored, &status))
			require.JSONEq(t, outcome, stored)
			require.Equal(t, "running", status, "upgrade must preserve unresolved physical work")
			var boundedPlanning bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT 'plan_cache_mode=force_custom_plan'=ANY(proconfig)
FROM pg_proc WHERE oid='container_topology_references(bigint[])'::regprocedure`).Scan(&boundedPlanning))
			require.True(t, boundedPlanning, "cleanup must receive its own migration after device-write outcomes")
		})
	}
}
