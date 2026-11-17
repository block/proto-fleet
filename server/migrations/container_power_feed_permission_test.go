package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestContainerPowerFeedPermissionMigrationGrantsExistingAdmins(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	ctx := t.Context()
	up, err := migrations.Current.ReadFile("current/001085_internal_container_power_feed_permission.up.sql")
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001085_internal_container_power_feed_permission.down.sql")
	require.NoError(t, err)

	grants := func() (admin, fieldTech int) {
		t.Helper()
		const q = `SELECT count(*) FROM role r JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id
			WHERE r.organization_id=$1 AND r.builtin_key=$2 AND r.deleted_at IS NULL AND p.key='container:power_feed'`
		require.NoError(t, database.DB.QueryRowContext(ctx, q, user.OrganizationID, "ADMIN").Scan(&admin))
		require.NoError(t, database.DB.QueryRowContext(ctx, q, user.OrganizationID, "FIELD_TECH").Scan(&fieldTech))
		return admin, fieldTech
	}

	// The org's roles exist before the permission does, as in a deployed fleet.
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	admin, _ := grants()
	require.Zero(t, admin)

	for range 2 { // idempotent
		_, err = database.DB.ExecContext(ctx, string(up))
		require.NoError(t, err)
		admin, fieldTech := grants()
		require.Equal(t, 1, admin, "existing ADMIN roles receive the permission")
		require.Zero(t, fieldTech, "FIELD_TECH needs an explicit grant")
	}

	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	admin, _ = grants()
	require.Zero(t, admin, "rollback removes the grant")
}
