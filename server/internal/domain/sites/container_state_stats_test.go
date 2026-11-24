package sites_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestContainerSiteStatsMatchesInventoryStateGroups(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	conn, ctx := database.DB, t.Context()
	org := database.CreateSuperAdminUser().OrganizationID
	controller := database.CreateDevice(org, "proto")
	site, err := sqlstores.NewSQLSiteStore(conn).CreateSite(ctx, models.CreateSiteParams{OrgID: org, Name: "State totals"})
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, site.ID, controller.DatabaseID)
	require.NoError(t, err)
	q := sqlc.New(conn)
	_, err = q.AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: controller.ID, ObservedAt: time.Now()})
	require.NoError(t, err)
	var tankID int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller.DatabaseID).Scan(&tankID))
	for i, state := range []string{"active", "inactive", "maintenance", "offline", "error", "needs_mining_pool", "updating", "reboot_required", "unknown"} {
		_, err = conn.ExecContext(ctx, `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at,observation_site_id) VALUES($1,$2,now(),now(),$3,now(),$4)`, tankID, i+1, state, site.ID)
		require.NoError(t, err)
	}
	// A stale hashing observation is unknown, not an offline module.
	_, err = conn.ExecContext(ctx, `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) VALUES($1,10,now(),now(),'active',now()-interval '1 hour')`, tankID)
	require.NoError(t, err)
	rows, err := q.ListContainerSiteStats(ctx, sqlc.ListContainerSiteStatsParams{OrgID: org, SiteIds: []int64{site.ID}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 10, rows[0].ModuleCount)
	require.EqualValues(t, 1, rows[0].HashingCount)
	require.EqualValues(t, 2, rows[0].SleepingCount)
	require.EqualValues(t, 1, rows[0].OfflineCount)
	require.EqualValues(t, 4, rows[0].BrokenCount)
}
