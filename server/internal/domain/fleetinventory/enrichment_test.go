package fleetinventory_test

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Move the device after page selection but before the real enrichment read.
// The stores and authorization checks both run against real database rows.
type movingMinerReader struct {
	fleetinventory.MinerReader
	move func()
}

func (r movingMinerReader) InventorySnapshots(ctx context.Context, orgID int64, ids []string, canReadSite func(*int64) bool) ([]*fleetpb.MinerStateSnapshot, error) {
	r.move()
	return r.MinerReader.InventorySnapshots(ctx, orgID, ids, canReadSite)
}

func TestEnrichmentReauthorizesCurrentMinerSite(t *testing.T) {
	infra := testutil.InitializeDBServiceInfrastructure(t)
	user := infra.DatabaseService.CreateSuperAdminUser()
	org := user.OrganizationID
	db := infra.ServiceProvider.DB
	ctx := t.Context()
	ids := infra.DatabaseService.CreateTestMiners(org, 2, "https://192.0.2.1:80")
	var siteA, siteB int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site (org_id,name,slug) VALUES ($1,'Allowed','allowed') RETURNING id", org).Scan(&siteA))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site (org_id,name,slug) VALUES ($1,'Restricted','restricted') RETURNING id", org).Scan(&siteB))

	for _, tc := range []struct {
		name        string
		scopes      []fleetinventory.Scope
		destination any
		allowed     bool
	}{
		{"allowlist denies reassignment", []fleetinventory.Scope{{Sites: []int64{siteA}}}, siteB, false},
		{"allowlist denies unassigned", []fleetinventory.Scope{{Sites: []int64{siteA}}}, nil, false},
		{"denylist denies reassignment", []fleetinventory.Scope{{OrgWide: true, Sites: []int64{siteB}}}, siteB, false},
		{"all permission scopes apply", []fleetinventory.Scope{{OrgWide: true}, {Sites: []int64{siteA}, TelemetryOnly: true}}, siteB, false},
		{"org-wide permits unassigned", []fleetinventory.Scope{{OrgWide: true}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE org_id=$2", siteA, org)
			require.NoError(t, err)
			reader := fleetinventory.NewService(sqlc.New(db)).WithMiners(movingMinerReader{
				MinerReader: infra.ServiceProvider.FleetManagementService,
				move: func() {
					_, err := db.ExecContext(ctx, "UPDATE device SET site_id=$1,custom_name='Moved detail' WHERE device_identifier=$2 AND org_id=$3", tc.destination, ids[0], org)
					require.NoError(t, err)
				},
			})
			result, err := reader.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}, tc.scopes)
			require.NoError(t, err)
			require.Len(t, result.Items, 2)
			for _, item := range result.Items {
				if item.DeviceIdentifier != ids[0] {
					require.NotNil(t, item.GetMiner(), "unchanged authorized miner keeps details")
					continue
				}
				if tc.allowed {
					require.NotNil(t, item.GetMiner())
					require.Equal(t, "Moved detail", item.GetMiner().Name)
					require.Nil(t, item.GetMiner().GetPlacement().GetSite())
				} else {
					require.Nil(t, item.GetMiner(), "enrichment must not attach details from a newly forbidden site")
				}
			}
		})
	}
}
