package sites_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

type emptyAttachmentTelemetry struct{}

func (emptyAttachmentTelemetry) GetLatestDeviceMetrics(context.Context, []minermodels.DeviceIdentifier) (map[minermodels.DeviceIdentifier]telemetrymodels.DeviceMetrics, error) {
	return nil, nil
}

func TestListSitesContainerAttachmentCounts(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	conn := database.DB
	org := database.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLSiteStore(conn)
	first, err := store.CreateSite(t.Context(), models.CreateSiteParams{OrgID: org, Name: "First"})
	require.NoError(t, err)
	second, err := store.CreateSite(t.Context(), models.CreateSiteParams{OrgID: org, Name: "Second"})
	require.NoError(t, err)
	controller := database.CreateDevice(org, "proto")
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, first.ID, controller.DatabaseID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,now())`, controller.DatabaseID, org)
	require.NoError(t, err)
	var tank int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller.DatabaseID).Scan(&tank))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,retired_at) VALUES($1,1,now(),now(),NULL),($1,2,now(),now(),NULL),($1,3,now(),now(),now())`, tank)
	require.NoError(t, err)
	var absentTank int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at,present) VALUES($1,2,now(),now(),false) RETURNING id`, controller.DatabaseID).Scan(&absentTank))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now())`, absentTank)
	require.NoError(t, err)

	svc := sites.NewService(store, nil, nil, sqlstores.NewSQLDeviceStore(conn), emptyAttachmentTelemetry{}, nil, nil).WithContainerStats(sqlc.New(conn))
	assertCounts := func(siteID, buildings, racks, miners int64) {
		t.Helper()
		for _, includeStats := range []bool{false, true} {
			reader := svc
			if !includeStats {
				reader = sites.NewService(store, nil, nil, nil, nil, nil, nil)
			}
			rows, err := reader.ListSites(t.Context(), org, fleetlistfilter.Filter{}, func(int64) bool { return includeStats })
			require.NoError(t, err)
			require.Len(t, rows, 2)
			for _, row := range rows {
				if row.Site.ID != siteID {
					continue
				}
				require.Equal(t, buildings, row.BuildingCount)
				require.Equal(t, racks, row.RackCount)
				require.Equal(t, miners, row.DeviceCount)
				if includeStats {
					require.NotNil(t, row.ListStats)
					require.EqualValues(t, buildings, row.ListStats.BuildingCount, "containers must not be counted twice")
					require.EqualValues(t, racks, row.ListStats.RackCount, "tanks must not be counted twice")
					require.EqualValues(t, miners, row.ListStats.DeviceCount)
				} else {
					require.Nil(t, row.ListStats)
				}
			}
		}
	}
	assertCounts(first.ID, 1, 1, 2)
	assertCounts(second.ID, 0, 0, 0)

	_, err = conn.ExecContext(t.Context(), `INSERT INTO building(org_id,site_id,name) VALUES($1,$2,'Building')`, org, first.ID)
	require.NoError(t, err)
	var rack int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO device_set(org_id,type,label) VALUES($1,'rack','Rack') RETURNING id`, org).Scan(&rack))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_set_rack(device_set_id,org_id,site_id,rows,columns) VALUES($1,$2,$3,1,1)`, rack, org, first.ID)
	require.NoError(t, err)
	miner := database.CreateDevice(org, "proto")
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, first.ID, miner.DatabaseID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, miner.DatabaseID)
	require.NoError(t, err)
	assertCounts(first.ID, 2, 2, 3)

	_, err = conn.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, second.ID, controller.DatabaseID)
	require.NoError(t, err)
	assertCounts(first.ID, 1, 1, 1)
	assertCounts(second.ID, 1, 1, 2)
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET site_id=NULL WHERE id=$1`, controller.DatabaseID)
	require.NoError(t, err)
	assertCounts(second.ID, 0, 0, 0)
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET site_id=$1,deleted_at=now() WHERE id=$2`, second.ID, controller.DatabaseID)
	require.NoError(t, err)
	assertCounts(second.ID, 0, 0, 0)

	otherOrg := database.CreateSuperAdminUser2().OrganizationID
	otherRows, err := svc.ListSites(t.Context(), otherOrg, fleetlistfilter.Filter{}, nil)
	require.NoError(t, err)
	require.Empty(t, otherRows)
}
