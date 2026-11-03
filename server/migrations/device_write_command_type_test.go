package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestWriteCommandMigrationPreservesUnclassifiedDirectAttempts(t *testing.T) {
	database, orgID, deviceID, message := writeFenceRollbackFixture(t)
	writes := devicewrite.NewService(database.DB)
	_, err := writes.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	_, err = writes.BeginHost(t.Context(), orgID, deviceID, 0)
	require.NoError(t, err)
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Current.ReadFile("current/001054_internal_pool_equipment_integration." + direction + ".sql")
		require.NoError(t, err)
		_, err = database.DB.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
	}
	var queued, direct string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT command_type FROM device_write_attempt WHERE queue_message_id=$1`, message.ID).Scan(&queued))
	require.Equal(t, "StopMining", queued, "backfill uses the original queued command")
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT command_type FROM device_write_attempt WHERE device_id=$1 AND queue_message_id IS NULL`, deviceID).Scan(&direct))
	require.Empty(t, direct, "legacy direct work must not be guessed from current device state")
	var running int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status='running'`, deviceID).Scan(&running))
	require.Equal(t, 2, running, "schema migration never resolves physical work")
}
