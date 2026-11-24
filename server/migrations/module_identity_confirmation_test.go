package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestModuleIdentityConfirmationMigrationRequiresFreshObservation(t *testing.T) {
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	conn, ctx := database.DB, t.Context()
	_, err = conn.ExecContext(ctx, `INSERT INTO container_inventory(device_id,observed_at,org_id) SELECT id,now(),org_id FROM device WHERE id=$1`, device.DatabaseID)
	require.NoError(t, err)
	var tankID, moduleID int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device.DatabaseID).Scan(&tankID))
	down, err := migrations.Current.ReadFile("current/001019_internal_container_module_identity_confirmation.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001019_internal_container_module_identity_confirmation.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'historical-module',now(),now()) RETURNING id`, tankID).Scan(&moduleID))
	for range 2 {
		_, err = conn.ExecContext(ctx, string(up))
		require.NoError(t, err)
		var identity string
		var confirmed bool
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT reported_id,identity_confirmed FROM container_module WHERE id=$1`, moduleID).Scan(&identity, &confirmed))
		require.Equal(t, "historical-module", identity)
		require.False(t, confirmed, "historical identity alone must not confirm current hardware")
		_, err = conn.ExecContext(ctx, string(down))
		require.NoError(t, err)
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT reported_id FROM container_module WHERE id=$1`, moduleID).Scan(&identity))
		require.Equal(t, "historical-module", identity)
	}
}
