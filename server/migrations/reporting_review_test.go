package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReportingReviewRollbackPreservesNotificationAcceptance(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	down, err := migrations.Current.ReadFile("current/001089_internal_hosted_reporting_delivery.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001089_internal_hosted_reporting_delivery.up.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	actor := database.CreateSuperAdminUser()
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint,notification_status,delivered_channel_ids)
 SELECT $1,$2,a.id,'test',$3,'{}','{}','retained','exhausted','{42}' FROM hosted_account a WHERE a.organization_id=$2 AND a.depth=0`, uuid.NewString(), actor.OrganizationID, actor.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "reporting notification history prevents rollback")
	var retained bool
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM hosted_report_queue WHERE delivered_channel_ids='{42}' AND notification_status='exhausted')`).Scan(&retained))
	require.True(t, retained)
}
