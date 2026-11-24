package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountAnalysisRollbackPreservesSavedLibrary(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	deletionDown, err := migrations.Current.ReadFile("current/001090_internal_account_analysis_deletion.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(deletionDown))
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001088_internal_hosted_account_analyses.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err, "empty analysis schema can roll back")
	up, err := migrations.Current.ReadFile("current/001088_internal_hosted_account_analyses.up.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	user := database.CreateSuperAdminUser()
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO hosted_account_analysis(analysis_id,organization_id,account_id,kind,snapshot,site_ids,ticket_ids,created_by,csv_data,sha256)
	 SELECT $1,$2,a.id,'miner_performance','{}','{}','{}',$3,'retained csv','digest'
	 FROM hosted_account a WHERE a.organization_id=$2 AND a.depth=0`, uuid.NewString(), user.OrganizationID, user.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "saved analyses prevent rollback")
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_analysis`).Scan(&count))
	require.Equal(t, 1, count)
	var immutable bool
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='hosted_account_analysis_immutable' AND NOT tgisinternal)`).Scan(&immutable))
	require.True(t, immutable)
}
