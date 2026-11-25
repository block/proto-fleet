package poolconfigurations

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOrganizationLockAllowsForeignKeysButSerializesPoolWriters(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = sqlc.New(writer).LockPoolConfigurationImportOrganization(ctx, org)
	require.NoError(t, err)

	t.Run("unrelated foreign key insert", func(t *testing.T) {
		tx, err := conn.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout = '500ms'`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `INSERT INTO pool(org_id,pool_name,url,username,password_enc) VALUES($1,'Unrelated','stratum+tcp://pool.example','worker','')`, org)
		require.NoError(t, err, "pool operations must not block organization foreign-key checks")
	})
	t.Run("another pool writer", func(t *testing.T) {
		tx, err := conn.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout = '100ms'`)
		require.NoError(t, err)
		_, err = sqlc.New(tx).LockPoolConfigurationImportOrganization(ctx, org)
		require.Error(t, err)
		var sqlError interface{ SQLState() string }
		require.ErrorAs(t, err, &sqlError)
		require.Equal(t, "55P03", sqlError.SQLState(), "pool writers still serialize")
	})
}
