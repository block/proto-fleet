package fleetinventory_test

import (
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerAncestorsComposeWithSiteAndTankScopes(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	org, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "container-filters", Name: "Filters"})
	require.NoError(t, err)
	north, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: org, Name: "North", Slug: "north"})
	require.NoError(t, err)
	south, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: org, Name: "South", Slug: "south"})
	require.NoError(t, err)
	metrics, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	addDevice := func(identifier string, site *int64, class commonpb.DeviceClass) int64 {
		t.Helper()
		d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: identifier, DeviceClass: class, Model: "Proto", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.1", Port: "8080", UrlScheme: "http"}, OrgID: org, IsActive: true}
		_, err := sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: identifier}, d)
		require.NoError(t, err)
		devices := sqlstores.NewSQLDeviceStore(db)
		require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, identifier))
		require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, "PAIRED"))
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, "UPDATE device SET site_id=$1, custom_name=$2 WHERE device_identifier=$2 RETURNING id", site, identifier).Scan(&id))
		if class == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
			now := time.Now().UTC().Truncate(time.Microsecond)
			tanks := []sdk.ContainerTank{}
			for position := int32(1); position <= 2; position++ {
				tanks = append(tanks, sdk.ContainerTank{Position: position, Modules: []sdk.ContainerModule{{Position: 1, State: sdk.ModuleHashing, ObservedAt: now}, {Position: 2, State: sdk.ModuleHashing, ObservedAt: now}}})
			}
			require.NoError(t, metrics.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: identifier, Timestamp: now, Container: &sdk.ContainerSnapshot{ObservedAt: now, TopologyComplete: true, Tanks: tanks}}))
		}
		return id
	}
	a := addDevice("A", &north.ID, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	b := addDevice("B", &south.ID, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	unassigned := addDevice("C", nil, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	rig := addDevice("Rig", &north.ID, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	// Deliberately collide the building's numeric ID with container A.
	_, err = db.ExecContext(ctx, "INSERT INTO building(id,org_id,site_id,name) VALUES($1,$2,$3,'Building')", a, org, north.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE device SET building_id=$1 WHERE id=$2", a, rig)
	require.NoError(t, err)
	service := fleetinventory.NewService(q)
	scopes := []fleetinventory.Scope{{OrgWide: true}}
	container := func(id int64) *pb.EntityRef { return &pb.EntityRef{Kind: pb.EntityKind_ENTITY_KIND_CONTAINER, Id: id} }
	var tankA, tankB int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM container_tank WHERE device_id=$1 AND position=1", a).Scan(&tankA))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM container_tank WHERE device_id=$1 AND position=1", b).Scan(&tankB))
	tank := func(id int64) *pb.EntityRef { return &pb.EntityRef{Kind: pb.EntityKind_ENTITY_KIND_TANK, Id: id} }
	for _, view := range []pb.InventoryView{pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS} {
		for _, tc := range []struct {
			name       string
			ancestors  []*pb.EntityRef
			sites      []int64
			unassigned bool
			count      int64
		}{
			{"one container", []*pb.EntityRef{container(a)}, nil, false, 2},
			{"multiple containers", []*pb.EntityRef{container(a), container(b)}, nil, false, 4},
			{"site intersection", []*pb.EntityRef{container(a), container(b)}, []int64{north.ID}, false, 2},
			{"contradictory site", []*pb.EntityRef{container(a)}, []int64{south.ID}, false, 0},
			{"unassigned", []*pb.EntityRef{container(unassigned)}, nil, true, 2},
			{"contradictory unassigned", []*pb.EntityRef{container(a)}, nil, true, 0},
		} {
			t.Run(fmt.Sprintf("%s/%s", view, tc.name), func(t *testing.T) {
				expected := tc.count
				if view == pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS {
					expected *= 2
				}
				req := &pb.ListInventoryRequest{View: view, Ancestors: tc.ancestors, SiteIds: tc.sites, IncludeUnassigned: tc.unassigned, PageSize: 1, Sort: pb.InventorySort_INVENTORY_SORT_NAME, Descending: true}
				seen := map[int64]bool{}
				for {
					result, err := service.List(ctx, org, req, scopes)
					require.NoError(t, err)
					require.Equal(t, expected, result.TotalCount)
					for _, item := range result.Items {
						require.False(t, seen[item.Ref.Id], "cursor must not duplicate rows")
						seen[item.Ref.Id] = true
						if view == pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS {
							require.Equal(t, pb.EntityKind_ENTITY_KIND_MODULE, item.Ref.Kind)
						} else {
							require.Equal(t, pb.EntityKind_ENTITY_KIND_TANK, item.Ref.Kind)
						}
					}
					req.PageToken = result.NextPageToken
					if req.PageToken == "" {
						break
					}
					require.Less(t, int64(len(seen)), expected)
				}
				require.Len(t, seen, int(expected))
			})
		}
	}
	for _, tc := range []struct {
		name      string
		ancestors []*pb.EntityRef
		count     int64
	}{
		{"matching tank", []*pb.EntityRef{container(a), tank(tankA)}, 2},
		{"contradictory tank", []*pb.EntityRef{container(a), tank(tankB)}, 0},
		{"multiple containers and tank", []*pb.EntityRef{container(a), container(b), tank(tankB)}, 2},
		{"tank only", []*pb.EntityRef{tank(tankA)}, 2},
		{"colliding building", []*pb.EntityRef{{Kind: pb.EntityKind_ENTITY_KIND_BUILDING, Id: a}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: tc.ancestors}, scopes)
			require.NoError(t, err)
			require.Equal(t, tc.count, result.TotalCount)
			if tc.name == "colliding building" {
				require.Equal(t, pb.EntityKind_ENTITY_KIND_MINER, result.Items[0].Ref.Kind)
			}
		})
	}
	result, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: []*pb.EntityRef{container(a)}, BuildingIds: []int64{a}}, scopes)
	require.NoError(t, err)
	require.Zero(t, result.TotalCount, "miner-only building filters cannot broaden module scope")
	result, err = service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: []*pb.EntityRef{container(b)}}, []fleetinventory.Scope{{Sites: []int64{north.ID}}})
	require.NoError(t, err)
	require.Zero(t, result.TotalCount, "selected containers cannot broaden site authorization")
}
