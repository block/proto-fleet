package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestHashboardWorkerNamesMigration(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	ctx := t.Context()
	up, err := migrations.Current.ReadFile("current/001076_internal_hashboard_worker_names.up.sql")
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001076_internal_hashboard_worker_names.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)

	var id int64
	require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, user.OrganizationID).Scan(&id))
	_, err = database.DB.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var name sql.NullString
	var mode string
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT worker_name,worker_name_mode FROM hashboard WHERE id=$1`, id).Scan(&name, &mode))
	require.False(t, name.Valid, "existing boards retain the plugin default")
	require.Equal(t, "none", mode)

	for _, tc := range []struct{ name, mode string }{
		{"", "none"}, {"aa:bb:cc:dd:ee:ff", "mac_address"}, {"rack-one", "custom"},
	} {
		_, err = database.DB.ExecContext(ctx, `UPDATE hashboard SET worker_name=$2,worker_name_mode=$3 WHERE id=$1`, id, tc.name, tc.mode)
		require.NoError(t, err)
		require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT worker_name,worker_name_mode FROM hashboard WHERE id=$1`, id).Scan(&name, &mode))
		require.Equal(t, sql.NullString{String: tc.name, Valid: true}, name)
		require.Equal(t, tc.mode, mode)
	}
	_, err = database.DB.ExecContext(ctx, `UPDATE hashboard SET worker_name_mode='invalid' WHERE id=$1`, id)
	require.ErrorContains(t, err, "hashboard_worker_name_mode_check")
	_, err = database.DB.ExecContext(ctx, `UPDATE hashboard SET worker_name_mode=NULL WHERE id=$1`, id)
	require.Error(t, err)

	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var columns, boards int
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='hashboard' AND column_name IN ('worker_name','worker_name_mode')`).Scan(&columns))
	require.Zero(t, columns, "rollback removes both columns")
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM hashboard WHERE id=$1`, id).Scan(&boards))
	require.Equal(t, 1, boards, "rollback preserves the physical board")
	_, err = database.DB.ExecContext(ctx, string(up))
	require.NoError(t, err, "migration can be reapplied after rollback")
}
