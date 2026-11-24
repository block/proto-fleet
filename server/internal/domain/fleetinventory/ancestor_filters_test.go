package fleetinventory_test

import (
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestConventionalInventoryPlacementMatchesNativeMinerReads(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	org, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "native-placement", Name: "Placement"})
	require.NoError(t, err)
	var building, otherBuilding int64
	for i, id := range []*int64{&building, &otherBuilding} {
		require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO building(org_id,name) VALUES($1,$2) RETURNING id", org, fmt.Sprintf("Building %d", i)).Scan(id))
	}
	devices := sqlstores.NewSQLDeviceStore(db)
	identifiers := map[int64]string{}
	for _, fixture := range []struct {
		identifier string
		building   *int64
		pairing    string
	}{
		{"inside", &building, "PAIRED"},
		{"unplaced", nil, "AUTHENTICATION_NEEDED"},
		{"other", &otherBuilding, "AUTHENTICATION_NEEDED"},
	} {
		d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: fixture.identifier, DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG, Model: "Proto", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.1", Port: "8080", UrlScheme: "http"}, OrgID: org, IsActive: true}
		_, err := sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: fixture.identifier}, d)
		require.NoError(t, err)
		require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, fixture.identifier))
		require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, fixture.pairing))
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, "UPDATE device SET building_id=$1, custom_name=$2 WHERE org_id=$3 AND device_identifier=$2 RETURNING id", fixture.building, fixture.identifier, org).Scan(&id))
		identifiers[id] = fixture.identifier
	}
	buildings := sqlstores.NewSQLBuildingStore(db)
	service := fleetinventory.NewService(q).WithBuildings(buildings)
	for _, tc := range []struct {
		name     string
		statuses []fleetpb.PairingStatus
		want     []string
	}{
		{"visible", []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_PAIRED, fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED}, []string{"inside", "unplaced"}},
		{"authentication needed", []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED}, []string{"unplaced"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := &fleetpb.MinerListFilter{BuildingIds: []int64{building}, IncludeNoRack: true, PairingStatuses: tc.statuses}
			nativeFilter, err := fleetmanagement.ParseListFilter(ctx, org, filter, buildings)
			require.NoError(t, err)
			rows, _, total, err := devices.ListMinerStateSnapshots(ctx, org, "", 100, nativeFilter, nil)
			require.NoError(t, err)
			var nativeIDs []string
			for _, row := range rows {
				nativeIDs = append(nativeIDs, row.DeviceIdentifier)
			}
			require.ElementsMatch(t, tc.want, nativeIDs)
			result, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, MinerFilter: filter}, []fleetinventory.Scope{{OrgWide: true}})
			require.NoError(t, err)
			var inventoryIDs []string
			for _, item := range result.Items {
				require.Equal(t, pb.EntityKind_ENTITY_KIND_MINER, item.Ref.Kind)
				inventoryIDs = append(inventoryIDs, identifiers[item.Ref.Id])
			}
			require.ElementsMatch(t, nativeIDs, inventoryIDs)
			require.Equal(t, total, result.TotalCount)
		})
	}
}

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
				tanks = append(tanks, sdk.ContainerTank{Position: position, Modules: []sdk.ContainerModule{{Position: 1, State: sdk.MinerStatusActive, ObservedAt: now}, {Position: 2, State: sdk.MinerStatusActive, ObservedAt: now}}})
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
	// A rack with the same numeric ID must remain a distinct ancestor.
	_, err = db.ExecContext(ctx, "INSERT INTO device_set (id,org_id,type,label) VALUES ($1,$2,'rack','Rack')", tankA, org)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO device_set_rack (device_set_id,org_id,site_id,building_id,rows,columns) VALUES ($1,$2,$3,$4,1,1)", tankA, org, north.ID, a)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO device_set_membership (org_id,device_set_id,device_set_type,device_id,device_identifier) SELECT $1,$2,'rack',id,device_identifier FROM device WHERE id=$3", org, tankA, rig)
	require.NoError(t, err)
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
		sites     []int64
		count     int64
	}{
		{"matching tank", []*pb.EntityRef{container(a), tank(tankA)}, nil, 2},
		{"contradictory tank", []*pb.EntityRef{container(a), tank(tankB)}, nil, 0},
		{"multiple containers and tank", []*pb.EntityRef{container(a), container(b), tank(tankB)}, nil, 2},
		{"tank only", []*pb.EntityRef{tank(tankA)}, nil, 2},
		{"multiple tanks with identical positions", []*pb.EntityRef{tank(tankA), tank(tankB)}, nil, 4},
		{"tank site intersection", []*pb.EntityRef{tank(tankA), tank(tankB)}, []int64{north.ID}, 2},
		{"contradictory tank site", []*pb.EntityRef{tank(tankA)}, []int64{south.ID}, 0},
		{"colliding building", []*pb.EntityRef{{Kind: pb.EntityKind_ENTITY_KIND_BUILDING, Id: a}}, nil, 1},
		{"mixed facilities", []*pb.EntityRef{container(a), {Kind: pb.EntityKind_ENTITY_KIND_BUILDING, Id: a}}, nil, 5},
		{"mixed groups", []*pb.EntityRef{tank(tankA), {Kind: pb.EntityKind_ENTITY_KIND_RACK, Id: tankA}}, nil, 3},
		{"mixed facilities and groups", []*pb.EntityRef{container(a), {Kind: pb.EntityKind_ENTITY_KIND_BUILDING, Id: a}, tank(tankA), {Kind: pb.EntityKind_ENTITY_KIND_RACK, Id: tankA}}, nil, 3},
		{"mixed contradictory site", []*pb.EntityRef{tank(tankA), {Kind: pb.EntityKind_ENTITY_KIND_RACK, Id: tankA}}, []int64{south.ID}, 0},
		{"colliding rack", []*pb.EntityRef{{Kind: pb.EntityKind_ENTITY_KIND_RACK, Id: tankA}}, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: tc.ancestors, SiteIds: tc.sites, PageSize: 1, Sort: pb.InventorySort_INVENTORY_SORT_NAME, Descending: true}
			seen := map[string]bool{}
			previousName := ""
			for {
				result, err := service.List(ctx, org, req, scopes)
				require.NoError(t, err)
				require.Equal(t, tc.count, result.TotalCount)
				for _, item := range result.Items {
					require.False(t, seen[fmt.Sprintf("%d:%d", item.Ref.Kind, item.Ref.Id)], "cursor must not duplicate rows")
					seen[fmt.Sprintf("%d:%d", item.Ref.Kind, item.Ref.Id)] = true
					if previousName != "" {
						require.LessOrEqual(t, item.Name, previousName)
					}
					previousName = item.Name
					if tc.name == "colliding building" || tc.name == "colliding rack" {
						require.Equal(t, pb.EntityKind_ENTITY_KIND_MINER, item.Ref.Kind)
					} else if tc.name != "mixed facilities" && tc.name != "mixed groups" && tc.name != "mixed facilities and groups" {
						require.Equal(t, pb.EntityKind_ENTITY_KIND_MODULE, item.Ref.Kind)
					}
				}
				req.PageToken = result.NextPageToken
				if req.PageToken == "" {
					break
				}
				require.Less(t, int64(len(seen)), tc.count)
			}
			require.Len(t, seen, int(tc.count))
		})
	}
	result, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: []*pb.EntityRef{container(a)}, BuildingIds: []int64{a}}, scopes)
	require.NoError(t, err)
	require.Zero(t, result.TotalCount, "miner-only building filters cannot broaden module scope")
	result, err = service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: []*pb.EntityRef{container(b)}}, []fleetinventory.Scope{{Sites: []int64{north.ID}}})
	require.NoError(t, err)
	require.Zero(t, result.TotalCount, "selected containers cannot broaden site authorization")

	t.Run("building-only equipment scope excludes colliding container tanks", func(t *testing.T) {
		request := &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Ancestors: []*pb.EntityRef{{Kind: pb.EntityKind_ENTITY_KIND_BUILDING, Id: a}}}
		result, err := service.List(ctx, org, request, scopes)
		require.NoError(t, err)
		require.EqualValues(t, 1, result.TotalCount)
		require.Len(t, result.Items, 1)
		require.Equal(t, &pb.EntityRef{Kind: pb.EntityKind_ENTITY_KIND_RACK, Id: tankA}, result.Items[0].Ref)
	})
	t.Run("mixed facility scope includes racks and tanks", func(t *testing.T) {
		request := &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Ancestors: []*pb.EntityRef{container(a), {Kind: pb.EntityKind_ENTITY_KIND_BUILDING, Id: a}}}
		result, err := service.List(ctx, org, request, scopes)
		require.NoError(t, err)
		require.EqualValues(t, 3, result.TotalCount)
	})
	t.Run("unassigned choices OR with typed scopes without broadening other categories", func(t *testing.T) {
		unracked := addDevice("Unracked", &north.ID, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		defer func() {
			_, err := db.ExecContext(ctx, "UPDATE device SET deleted_at=now() WHERE id=$1", unracked)
			require.NoError(t, err)
		}()
		_, err := db.ExecContext(ctx, "UPDATE device_set_rack SET building_id=NULL WHERE device_set_id=$1", tankA)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "UPDATE device SET building_id=NULL WHERE id=$1", rig)
		require.NoError(t, err)
		defer func() {
			_, err := db.ExecContext(ctx, "UPDATE device_set_rack SET building_id=$1 WHERE device_set_id=$2", a, tankA)
			require.NoError(t, err)
		}()
		t.Run("unassigned-only equipment scope excludes all tanks", func(t *testing.T) {
			result, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, IncludeNoFacility: true}, scopes)
			require.NoError(t, err)
			require.EqualValues(t, 1, result.TotalCount)
			require.Len(t, result.Items, 1)
			require.Equal(t, &pb.EntityRef{Kind: pb.EntityKind_ENTITY_KIND_RACK, Id: tankA}, result.Items[0].Ref)
		})
		for _, tc := range []struct {
			name  string
			req   *pb.ListInventoryRequest
			count int64
		}{
			{"unassigned facility", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoFacility: true}, 1},
			{"unassigned group", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoGroup: true}, 1},
			{"facility or container", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoFacility: true, Ancestors: []*pb.EntityRef{container(a)}}, 5},
			{"group or tank", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoGroup: true, Ancestors: []*pb.EntityRef{tank(tankA)}}, 3},
			{"combined unassigned scopes", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoFacility: true, IncludeNoGroup: true}, 1},
			{"group with contradictory facility", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoGroup: true, Ancestors: []*pb.EntityRef{container(a), tank(tankB)}}, 0},
			{"racks without building or container", &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, IncludeNoFacility: true, Ancestors: []*pb.EntityRef{container(a)}}, 3},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result, err := service.List(ctx, org, tc.req, scopes)
				require.NoError(t, err)
				require.Equal(t, tc.count, result.TotalCount)
			})
		}
		for _, req := range []*pb.ListInventoryRequest{{View: pb.InventoryView_INVENTORY_VIEW_FACILITIES, IncludeNoFacility: true}, {View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, IncludeNoGroup: true}, {View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, IncludeNoGroup: true, ZoneKeys: []*commonpb.ZoneKey{{Zone: "A"}}}} {
			_, err := service.List(ctx, org, req, scopes)
			require.Error(t, err)
		}
	})

	t.Run("lightweight tank catalog matches readable inventory labels across cursor pages", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "UPDATE device SET custom_name='A' WHERE id=$1", b)
		require.NoError(t, err, "duplicate names must use stable tank IDs as the cursor tie-breaker")
		for _, authority := range [][]fleetinventory.Scope{scopes, {{Sites: []int64{north.ID}}}, {{OrgWide: true, Sites: []int64{south.ID}}}, {{Sites: []int64{}}}} {
			inventory, err := service.List(ctx, org, &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Kinds: []pb.EntityKind{pb.EntityKind_ENTITY_KIND_TANK}, PageSize: 1000, Sort: pb.InventorySort_INVENTORY_SORT_NAME}, authority)
			require.NoError(t, err)
			var options []*pb.TankOption
			request := &pb.ListTankOptionsRequest{PageSize: 1}
			for {
				catalog, err := service.TankOptions(ctx, org, request, authority)
				require.NoError(t, err)
				options = append(options, catalog.Options...)
				if catalog.NextPageToken == "" {
					break
				}
				request.PageToken = catalog.NextPageToken
				require.Less(t, len(options), len(inventory.Items), "cursor must make progress")
			}
			require.Len(t, options, len(inventory.Items))
			for i, item := range inventory.Items {
				require.Equal(t, &pb.TankOption{Id: item.Ref.Id, Name: item.Name, SiteName: item.SiteName}, options[i])
			}
		}
		first, err := service.TankOptions(ctx, org, &pb.ListTankOptionsRequest{PageSize: 1}, scopes)
		require.NoError(t, err)
		require.NotEmpty(t, first.NextPageToken)
		_, err = service.TankOptions(ctx, org, &pb.ListTankOptionsRequest{PageToken: first.NextPageToken}, []fleetinventory.Scope{{Sites: []int64{north.ID}}})
		require.Error(t, err, "catalog cursor must be bound to current permissions")
		_, err = service.TankOptions(ctx, org+1, &pb.ListTankOptionsRequest{PageToken: first.NextPageToken}, scopes)
		require.Error(t, err, "catalog cursor must be bound to current organization")
		otherOrg, err := service.TankOptions(ctx, org+1, &pb.ListTankOptionsRequest{}, scopes)
		require.NoError(t, err)
		require.Empty(t, otherOrg.Options)
	})
	t.Run("catalog retains empty tanks and excludes absent or deleted controllers", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "UPDATE container_module SET retired_at=now() WHERE tank_id=$1", tankA)
		require.NoError(t, err)
		catalog, err := service.TankOptions(ctx, org, &pb.ListTankOptionsRequest{}, scopes)
		require.NoError(t, err)
		require.Len(t, catalog.Options, 6, "labels do not depend on module membership")
		_, err = db.ExecContext(ctx, "UPDATE container_tank SET present=false WHERE id=$1", tankB)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "UPDATE device SET deleted_at=now() WHERE id=$1", unassigned)
		require.NoError(t, err)
		catalog, err = service.TankOptions(ctx, org, &pb.ListTankOptionsRequest{}, scopes)
		require.NoError(t, err)
		require.Len(t, catalog.Options, 3)
	})
}
