package containers_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	errorpb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	inventorypb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestInventoryRetainsPartialTopologyAndModuleHistory(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "inventory", Name: "Inventory"})
	require.NoError(t, err)
	d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "inventory-ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, SerialNumber: "inventory-serial", Model: "CT1", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.11", Port: "8080", UrlScheme: "http"}, OrgID: org, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: d.DeviceIdentifier}, d)
	require.NoError(t, err)
	devices := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, d.DeviceIdentifier))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, "PAIRED"))
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hashrate, power := 300e12, 3600.0
	snapshot := &sdk.ContainerSnapshot{ObservedAt: now, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{
		{Position: 1, ReportedID: "one", State: sdk.ModuleHashing, ObservedAt: now, Metrics: &sdk.ContainerModuleMetrics{ObservedAt: now, HashrateHS: &hashrate, PowerW: &power}},
		{Position: 2, ReportedID: "two", State: sdk.ModuleSleeping, ObservedAt: now},
	}}}}
	write := func() {
		t.Helper()
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: snapshot.ObservedAt, Container: snapshot}))
	}
	count := func(query string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, query).Scan(&n))
		return n
	}
	write()
	require.Equal(t, 1, count("SELECT count(*) FROM container_tank WHERE present"))
	require.Equal(t, 2, count("SELECT count(*) FROM container_module WHERE retired_at IS NULL"))
	write() // retried transport must not duplicate history or inventory
	require.Equal(t, 1, count("SELECT count(*) FROM container_module_metrics"))

	// Global paging and filtering use the same database relation as the count.
	reader := fleetinventory.NewService(sqlc.New(db))
	scopes := []fleetinventory.Scope{{OrgWide: true}}
	t.Run("inventory names follow container renames in search and paging", func(t *testing.T) {
		t.Cleanup(func() {
			_, err := db.ExecContext(ctx, "UPDATE device SET custom_name = NULL, serial_number = $1 WHERE device_identifier = $2", d.SerialNumber, d.DeviceIdentifier)
			require.NoError(t, err)
		})
		for _, tc := range []struct {
			customName, serial, containerName string
		}{
			{"", d.SerialNumber, "Container inventory-serial"},
			{"North CT1", d.SerialNumber, "North CT1"},
			{"South CT1", d.SerialNumber, "South CT1"},
			{"", "", "Container inventory-ct1"},
		} {
			_, err := db.ExecContext(ctx, "UPDATE device SET custom_name = $1, serial_number = $2 WHERE device_identifier = $3", tc.customName, tc.serial, d.DeviceIdentifier)
			require.NoError(t, err)
			request := &inventorypb.ListInventoryRequest{
				View:        inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS,
				SearchQuery: tc.containerName + " T1", PageSize: 1,
				Sort: inventorypb.InventorySort_INVENTORY_SORT_NAME,
			}
			first, err := reader.List(ctx, org, request, scopes)
			require.NoError(t, err)
			require.EqualValues(t, 2, first.TotalCount)
			require.Len(t, first.Items, 1)
			require.Equal(t, tc.containerName+" T1 M1", first.Items[0].Name)
			request.PageToken = first.NextPageToken
			second, err := reader.List(ctx, org, request, scopes)
			require.NoError(t, err)
			require.Len(t, second.Items, 1)
			require.Equal(t, tc.containerName+" T1 M2", second.Items[0].Name)
			require.Empty(t, second.NextPageToken)
			tanks, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{
				View:        inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS,
				SearchQuery: tc.containerName + " T1",
			}, scopes)
			require.NoError(t, err)
			require.Len(t, tanks.Items, 1)
			require.Equal(t, tc.containerName+" T1", tanks.Items[0].Name)
		}
	})
	req := &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, PageSize: 1}
	page, err := reader.List(ctx, org, req, scopes)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalCount)
	require.Len(t, page.Items, 1)
	moduleID := page.Items[0].Ref.Id
	require.Equal(t, inventorypb.EntityKind_ENTITY_KIND_MODULE, page.Items[0].Ref.Kind)
	require.NotNil(t, page.Items[0].HashrateThs, "fresh module must report hashrate: %+v", page.Items[0])
	require.Equal(t, 300.0, *page.Items[0].HashrateThs)
	req.PageToken = page.NextPageToken
	page, err = reader.List(ctx, org, req, scopes)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotEqual(t, moduleID, page.Items[0].Ref.Id)
	require.Empty(t, page.NextPageToken)
	req.PageToken = ""
	req.TelemetryRanges = []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS, Min: wrapperspb.Double(100)}}
	page, err = reader.List(ctx, org, req, scopes)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalCount)
	require.Equal(t, moduleID, page.Items[0].Ref.Id)
	denied, err := reader.List(ctx, org, req, []fleetinventory.Scope{{OrgWide: false}})
	require.NoError(t, err)
	require.Zero(t, denied.TotalCount)
	require.Empty(t, denied.Items)
	history, err := reader.History(ctx, org, &inventorypb.ListModuleHistoryRequest{ModuleId: moduleID, StartTime: timestamppb.New(now.Add(-time.Hour)), EndTime: timestamppb.New(now.Add(time.Hour))}, scopes)
	require.NoError(t, err)
	require.Len(t, history.Samples, 1)
	require.Equal(t, 3600.0, *history.Samples[0].PowerW)
	historyRequest := &inventorypb.ListModuleHistoryRequest{ModuleId: moduleID, StartTime: timestamppb.New(now.Add(-time.Hour)), EndTime: timestamppb.New(now.Add(time.Hour))}
	_, err = reader.History(ctx, org, historyRequest, []fleetinventory.Scope{{OrgWide: false}})
	require.Error(t, err, "history must enforce site scope independently of list access")
	otherOrg, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "other-inventory", Name: "Other inventory"})
	require.NoError(t, err)
	_, err = reader.History(ctx, otherOrg, historyRequest, scopes)
	require.Error(t, err, "history must reject a module in another organization")
	otherPage, err := reader.List(ctx, otherOrg, req, scopes)
	require.NoError(t, err)
	require.Zero(t, otherPage.TotalCount)
	require.Empty(t, otherPage.Items)

	// A conventional miner participates in the same ordering, count, and cursor.
	miner := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "inventory-miner", SerialNumber: "miner-serial", Model: "S21", Manufacturer: "Z", DriverName: "antminer", IpAddress: "192.0.2.12", Port: "4028", UrlScheme: "http"}, OrgID: org, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: miner.DeviceIdentifier}, miner)
	require.NoError(t, err)
	require.NoError(t, devices.InsertDevice(ctx, &miner.Device, org, miner.DeviceIdentifier))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &miner.Device, org, "PAIRED"))
	require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: miner.DeviceIdentifier, Timestamp: now, HashrateHS: &telemetry.MetricValue{Value: 80e12}, PowerW: &telemetry.MetricValue{Value: 2400}, EfficiencyJH: &telemetry.MetricValue{Value: 25e-12}}))
	for _, filter := range []*fleetpb.MinerListFilter{
		{Models: []string{"S21"}},
		{IpCidrs: []string{"192.0.2.99/24"}},
		{IpRanges: []*fleetpb.IpRange{{StartIp: "192.0.2.10", EndIp: "192.0.2.20"}}},
		{SearchQuery: "192.0.2.12"},
		{PairingStatuses: []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_PAIRED}},
		{NumericRanges: []*fleetpb.NumericRangeFilter{{Field: fleetpb.NumericField_NUMERIC_FIELD_POWER_KW, Min: wrapperspb.Double(2), Max: wrapperspb.Double(3)}}},
	} {
		result, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, MinerFilter: filter}, scopes)
		require.NoError(t, err, "filter: %v", filter)
		require.EqualValues(t, 1, result.TotalCount, "miner-specific filters must not admit modules: %v", filter)
		require.Equal(t, inventorypb.EntityKind_ENTITY_KIND_MINER, result.Items[0].Ref.Kind)
	}
	efficiencyPage, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_EFFICIENCY_JTH, Min: wrapperspb.Double(24), Max: wrapperspb.Double(26)}}}, scopes)
	require.NoError(t, err)
	require.EqualValues(t, 1, efficiencyPage.TotalCount, "miner filters use its measured efficiency, not power/hashrate (30)")
	require.Equal(t, 25.0, *efficiencyPage.Items[0].EfficiencyJth)
	for _, descending := range []bool{false, true} {
		request := &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, PageSize: 1, Sort: inventorypb.InventorySort_INVENTORY_SORT_HASHRATE, Descending: descending}
		var kinds []inventorypb.EntityKind
		seen := map[string]bool{}
		for {
			result, err := reader.List(ctx, org, request, scopes)
			require.NoError(t, err)
			require.EqualValues(t, 3, result.TotalCount)
			require.Len(t, result.Items, 1)
			key := result.Items[0].Ref.String()
			require.False(t, seen[key], "cursor returned a duplicate row")
			seen[key] = true
			kinds = append(kinds, result.Items[0].Ref.Kind)
			if result.NextPageToken == "" {
				break
			}
			request.PageToken = result.NextPageToken
			require.Less(t, len(kinds), 4)
		}
		if descending {
			require.Equal(t, inventorypb.EntityKind_ENTITY_KIND_MODULE, kinds[0])
		} else {
			require.Equal(t, inventorypb.EntityKind_ENTITY_KIND_MINER, kinds[0])
		}
		require.Len(t, kinds, 3)
	}

	var buildingID, rackID int64
	// Missing controller serial numbers must still yield a sortable name.
	_, err = db.ExecContext(ctx, "UPDATE device SET serial_number = NULL WHERE device_identifier = $1", d.DeviceIdentifier)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO building (org_id, name) VALUES ($1, 'Building') RETURNING id", org).Scan(&buildingID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO device_set (org_id, type, label) VALUES ($1, 'rack', 'Rack') RETURNING id", org).Scan(&rackID))
	_, err = db.ExecContext(ctx, "INSERT INTO device_set_rack (device_set_id, org_id, building_id, rows, columns) VALUES ($1,$2,$3,1,1)", rackID, org, buildingID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO device_set_membership (org_id,device_set_id,device_set_type,device_id,device_identifier) SELECT $1,$2,'rack',id,device_identifier FROM device WHERE device_identifier=$3", org, rackID, miner.DeviceIdentifier)
	require.NoError(t, err)
	for _, view := range []inventorypb.InventoryView{inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES, inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS} {
		result, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: view}, scopes)
		require.NoError(t, err)
		require.EqualValues(t, 2, result.TotalCount)
		require.Len(t, result.Items, 2)
		for _, item := range result.Items {
			require.NotEmpty(t, item.Name)
			if item.Ref.Kind == inventorypb.EntityKind_ENTITY_KIND_BUILDING || item.Ref.Kind == inventorypb.EntityKind_ENTITY_KIND_RACK {
				require.NotNil(t, item.HashrateThs)
				require.Equal(t, 80.0, *item.HashrateThs)
			} else if item.Ref.Kind == inventorypb.EntityKind_ENTITY_KIND_TANK {
				require.NotNil(t, item.HashrateThs)
				require.Equal(t, 300.0, *item.HashrateThs, "tank totals exclude modules without readings")
				require.EqualValues(t, 1, item.HashrateReportingCount)
				require.EqualValues(t, 2, item.AssetCount)
			} else {
				require.Nil(t, item.HashrateThs, "container totals still require complete coverage")
			}
		}
	}
	t.Run("parent lists preserve mixed cursors counts and scope", func(t *testing.T) {
		for _, view := range []inventorypb.InventoryView{inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES, inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS} {
			for _, sort := range []inventorypb.InventorySort{inventorypb.InventorySort_INVENTORY_SORT_NAME, inventorypb.InventorySort_INVENTORY_SORT_HASHRATE, inventorypb.InventorySort_INVENTORY_SORT_TEMPERATURE} {
				for _, descending := range []bool{false, true} {
					request := &inventorypb.ListInventoryRequest{View: view, Sort: sort, Descending: descending, PageSize: 1}
					seen := map[string]bool{}
					for {
						page, err := reader.List(ctx, org, request, scopes)
						require.NoError(t, err)
						require.EqualValues(t, 2, page.TotalCount)
						require.Zero(t, page.MinerCount)
						require.Len(t, page.Items, 1)
						if sort == inventorypb.InventorySort_INVENTORY_SORT_HASHRATE && len(seen) == 0 {
							require.NotNil(t, page.Items[0].HashrateThs, "unavailable totals sort last in both directions")
						}
						key := page.Items[0].Ref.String()
						require.False(t, seen[key], "duplicate parent with view=%v sort=%v descending=%v", view, sort, descending)
						seen[key] = true
						if page.NextPageToken == "" {
							break
						}
						require.Less(t, len(seen), 2)
						request.PageToken = page.NextPageToken
					}
					require.Len(t, seen, 2, "both entity types survive pagination, including null telemetry ties")
				}
			}
			request := &inventorypb.ListInventoryRequest{View: view, TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS, Min: wrapperspb.Double(70), Max: wrapperspb.Double(90)}}}
			filtered, err := reader.List(ctx, org, request, scopes)
			require.NoError(t, err)
			require.EqualValues(t, 1, filtered.TotalCount)
			require.Len(t, filtered.Items, 1)
			require.Equal(t, 80.0, *filtered.Items[0].HashrateThs)
			require.Equal(t, 30.0, *filtered.Items[0].EfficiencyJth, "parent efficiency uses total power/hashrate, not miner-reported 25")
			for _, authority := range [][]fleetinventory.Scope{{{}}, {{OrgWide: true}, {Sites: []int64{999999}}}} {
				denied, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: view}, authority)
				require.NoError(t, err)
				require.Zero(t, denied.TotalCount)
				require.Empty(t, denied.Items)
			}
			other, err := reader.List(ctx, otherOrg, &inventorypb.ListInventoryRequest{View: view}, scopes)
			require.NoError(t, err)
			require.Zero(t, other.TotalCount)
			require.Empty(t, other.Items)
		}
	})
	t.Run("equipment groups respect typed facility ancestors", func(t *testing.T) {
		groups, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS}, scopes)
		require.NoError(t, err)
		require.Len(t, groups.Items, 2)
		for _, group := range groups.Items {
			require.Len(t, group.Ancestors, 1)
			filtered, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Ancestors: group.Ancestors}, scopes)
			require.NoError(t, err)
			require.EqualValues(t, 1, filtered.TotalCount)
			require.Len(t, filtered.Items, 1)
			require.Equal(t, group.Ref, filtered.Items[0].Ref)
		}
	})
	selected, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Ancestors: []*inventorypb.EntityRef{{Kind: inventorypb.EntityKind_ENTITY_KIND_BUILDING, Id: buildingID}}}, scopes)
	require.NoError(t, err)
	require.EqualValues(t, 1, selected.TotalCount)
	require.Equal(t, inventorypb.EntityKind_ENTITY_KIND_MINER, selected.Items[0].Ref.Kind)

	// Typed placement and issue filters select conventional groups only. Tanks
	// must never inherit a rack's zone, building, or miner component errors.
	_, err = db.ExecContext(ctx, "UPDATE device_set_rack SET zone='north' WHERE device_set_id=$1", rackID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO errors (error_id,org_id,miner_error,severity,summary,first_seen_at,last_seen_at,device_id,component_type)
	 SELECT 'inventory-error',$1,1,1,'Fan failure',now(),now(),id,2 FROM device WHERE device_identifier=$2`, org, miner.DeviceIdentifier)
	require.NoError(t, err)
	for _, request := range []*inventorypb.ListInventoryRequest{
		{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, BuildingIds: []int64{buildingID}},
		{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, ZoneKeys: []*commonpb.ZoneKey{{BuildingId: buildingID, Zone: "north"}}},
		{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, ErrorComponentTypes: []errorpb.ComponentType{2}},
		{View: inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES, ErrorComponentTypes: []errorpb.ComponentType{2}},
		{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, States: []string{"offline"}},
	} {
		result, listErr := reader.List(ctx, org, request, scopes)
		require.NoError(t, listErr)
		require.EqualValues(t, 1, result.TotalCount, "filter: %v", request)
		require.NotContains(t, []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_CONTAINER, inventorypb.EntityKind_ENTITY_KIND_TANK, inventorypb.EntityKind_ENTITY_KIND_MODULE}, result.Items[0].Ref.Kind)
	}
	noMatch, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, ZoneKeys: []*commonpb.ZoneKey{{BuildingId: buildingID + 1000, Zone: "north"}}}, scopes)
	require.NoError(t, err)
	require.Zero(t, noMatch.TotalCount, "zone text cannot match a different building")

	t.Run("authorized options and metadata independent of telemetry", func(t *testing.T) {
		var siteA, siteB, buildingB int64
		require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site (org_id,name,slug) VALUES ($1,'Site A','site-a') RETURNING id", org).Scan(&siteA))
		require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO site (org_id,name,slug) VALUES ($1,'Site B','site-b') RETURNING id", org).Scan(&siteB))
		_, err := db.ExecContext(ctx, "UPDATE building SET site_id=$1 WHERE id=$2", siteA, buildingID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE device_identifier=$2", siteA, miner.DeviceIdentifier)
		require.NoError(t, err)
		require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO building (org_id,name,site_id) VALUES ($1,'Other building',$2) RETURNING id", org, siteB).Scan(&buildingB))
		other := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "restricted-miner", Model: "RestrictedModel", FirmwareVersion: "RestrictedFirmware", Manufacturer: "Z", DriverName: "antminer", IpAddress: "192.0.2.13", Port: "4028", UrlScheme: "http"}, OrgID: org, IsActive: true}
		_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: other.DeviceIdentifier}, other)
		require.NoError(t, err)
		require.NoError(t, devices.InsertDevice(ctx, &other.Device, org, other.DeviceIdentifier))
		require.NoError(t, devices.UpsertDevicePairing(ctx, &other.Device, org, "PAIRED"))
		_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1,building_id=$2 WHERE device_identifier=$3", siteB, buildingB, other.DeviceIdentifier)
		require.NoError(t, err)
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: other.DeviceIdentifier, Timestamp: now, HashrateHS: &telemetry.MetricValue{Value: 500e12}, PowerW: &telemetry.MetricValue{Value: 9000}}))
		request := &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS}
		for _, authority := range [][]fleetinventory.Scope{
			{{OrgWide: true}, {Sites: []int64{siteA}}},
			{{OrgWide: true, Sites: []int64{siteB}}},
		} {
			result, err := reader.List(ctx, org, request, authority)
			require.NoError(t, err)
			require.Equal(t, []string{"S21"}, result.Models)
			require.Empty(t, result.FirmwareVersions)
		}
		denied, err := reader.List(ctx, org, request, []fleetinventory.Scope{{}})
		require.NoError(t, err)
		require.Empty(t, denied.Models)
		require.Empty(t, denied.FirmwareVersions)
		authority := []fleetinventory.Scope{{OrgWide: true}, {OrgWide: true, Sites: []int64{siteB}, TelemetryOnly: true}}
		buildings, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES, Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_BUILDING}}, authority)
		require.NoError(t, err)
		require.EqualValues(t, 2, buildings.TotalCount)
		for _, item := range buildings.Items {
			require.NotNil(t, item.GetBuilding())
			if item.Ref.Id == buildingB {
				require.Equal(t, "Other building", item.Name)
				require.Nil(t, item.ListStats)
				require.Nil(t, item.GetBuilding().ListStats)
				require.Nil(t, item.HashrateThs)
				require.Nil(t, item.PowerKw)
			} else {
				require.NotNil(t, item.ListStats)
			}
		}
		filtered, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES, Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_BUILDING}, TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS, Min: wrapperspb.Double(100)}}}, authority)
		require.NoError(t, err)
		require.Zero(t, filtered.TotalCount, "restricted telemetry must not leak through filter membership")
		// Three error records on one component still display/sort as one.
		_, err = db.ExecContext(ctx, `INSERT INTO errors (error_id,org_id,miner_error,severity,summary,first_seen_at,last_seen_at,device_id,component_type)
     SELECT 'duplicate-'||n,$1,1,1,'failure',now(),now(),d.id,2 FROM device d CROSS JOIN generate_series(1,2) n WHERE device_identifier=$2`, org, miner.DeviceIdentifier)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO errors (error_id,org_id,miner_error,severity,summary,first_seen_at,last_seen_at,device_id,component_type)
     SELECT 'distinct-'||n,$1,1,1,'failure',now(),now(),d.id,n FROM device d CROSS JOIN generate_series(3,4) n WHERE device_identifier=$2`, org, other.DeviceIdentifier)
		require.NoError(t, err)
		sorted, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES, Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_BUILDING}, Sort: inventorypb.InventorySort_INVENTORY_SORT_ISSUE_COUNT, Descending: true}, scopes)
		require.NoError(t, err)
		require.Equal(t, buildingB, sorted.Items[0].Ref.Id)
		require.Equal(t, buildingID, sorted.Items[1].Ref.Id)
	})

	snapshot.TopologyComplete = false
	snapshot.ObservedAt = now.Add(time.Second)
	snapshot.Tanks[0].Modules = snapshot.Tanks[0].Modules[:1]
	write()
	require.Equal(t, 2, count("SELECT count(*) FROM container_module WHERE retired_at IS NULL"))
	cardReader := containers.NewService(sqlstores.NewSQLContainerStore(db))
	card, err := cardReader.Get(ctx, org, d.DeviceIdentifier, func(*int64) (bool, error) { return true, nil })
	require.NoError(t, err)
	require.Len(t, card.Tanks, 1)
	require.EqualValues(t, 1, card.Tanks[0].HashingCount)
	require.EqualValues(t, 1, card.Tanks[0].SleepingCount, "a partial poll must retain the same module membership as inventory")

	// An older source sample enters history without replacing the latest value.
	snapshot.ObservedAt = now.Add(2 * time.Second)
	snapshot.Tanks[0].Modules[0].Metrics.ObservedAt = now.Add(-time.Minute)
	snapshot.Tanks[0].Modules[0].Metrics.PowerW = nil
	snapshot.Tanks[0].Modules[0].ObservedAt = now.Add(-time.Minute)
	snapshot.Tanks[0].Modules[0].State = sdk.ModuleOffline
	write()
	require.Equal(t, 3, count("SELECT count(*) FROM container_module_metrics"))
	var latestPower float64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT power_w FROM container_module_latest_metrics").Scan(&latestPower))
	require.Equal(t, power, latestPower)
	var currentState string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT state FROM container_module WHERE id = $1", moduleID).Scan(&currentState))
	require.Equal(t, string(sdk.ModuleHashing), currentState, "older state evidence must not replace newer state")
	card, err = cardReader.Get(ctx, org, d.DeviceIdentifier, func(*int64) (bool, error) { return true, nil })
	require.NoError(t, err)
	require.EqualValues(t, 1, card.Tanks[0].HashingCount, "cards must use reconciled state rather than the latest raw observation")
	require.Zero(t, card.Tanks[0].OfflineCount)

	// Only a newer complete inventory retires absent members.
	snapshot.TopologyComplete = true
	snapshot.ObservedAt = now.Add(3 * time.Second)
	snapshot.Tanks[0].Modules[0].Metrics = nil
	write()
	require.Equal(t, 1, count("SELECT count(*) FROM container_module WHERE retired_at IS NULL"))
	card, err = cardReader.Get(ctx, org, d.DeviceIdentifier, func(*int64) (bool, error) { return true, nil })
	require.NoError(t, err)
	require.EqualValues(t, 1, card.Tanks[0].HashingCount)
	require.Zero(t, card.Tanks[0].SleepingCount, "retired modules must not count toward card health")

	// A known hardware replacement in the same selector gets a new Fleet ID.
	snapshot.ObservedAt = now.Add(4 * time.Second)
	snapshot.Tanks[0].Modules[0].ReportedID = "replacement"
	snapshot.Tanks[0].Modules[0].Metrics = nil
	write()
	require.Equal(t, 3, count("SELECT count(*) FROM container_module"))
	require.Equal(t, 1, count("SELECT count(*) FROM container_module WHERE retired_at IS NULL"))
	require.Equal(t, 3, count("SELECT count(*) FROM container_module_metrics"))
	// Source-time ties page using collection time without dropping observations.
	historyReq := &inventorypb.ListModuleHistoryRequest{ModuleId: moduleID, StartTime: timestamppb.New(now.Add(-time.Hour)), EndTime: timestamppb.New(now.Add(time.Hour)), PageSize: 1}
	var times []time.Time
	for {
		page, err := reader.History(ctx, org, historyReq, scopes)
		require.NoError(t, err)
		require.Len(t, page.Samples, 1)
		times = append(times, page.Samples[0].CollectedAt.AsTime())
		if page.NextPageToken == "" {
			break
		}
		historyReq.PageToken = page.NextPageToken
		require.Less(t, len(times), 4)
	}
	require.Len(t, times, 3)

	snapshot.ObservedAt = now // delayed old inventory cannot resurrect a retired module
	snapshot.Tanks[0].Modules[0].ReportedID = "one"
	write()
	require.Equal(t, 1, count("SELECT count(*) FROM container_module WHERE retired_at IS NULL AND reported_id = 'replacement'"))
	t.Run("missing evidence cannot revive old state or metrics", func(t *testing.T) {
		// Use microsecond steps above the last accepted poll without future skew.
		snapshot.ObservedAt = now.Add(4*time.Second + time.Microsecond)
		module := &snapshot.Tanks[0].Modules[0]
		module.ReportedID = "replacement"
		module.State = sdk.ModuleHashing
		module.ObservedAt = now
		module.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: now, HashrateHS: &hashrate, PowerW: &power}
		write()
		// A later poll can carry older empty evidence. Keep the newer valid
		// measurement and its parent reporting coverage in that case.
		snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Microsecond)
		module.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: now.Add(-30 * time.Second)}
		write()
		retained, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_TANK}}, scopes)
		require.NoError(t, err)
		require.Len(t, retained.Items, 1)
		require.EqualValues(t, 1, retained.Items[0].HashrateReportingCount)
		require.EqualValues(t, 1, retained.Items[0].PowerReportingCount)
		require.NotNil(t, retained.Items[0].HashrateThs)
		require.NotNil(t, retained.Items[0].PowerKw)
		require.Equal(t, hashrate/1e12, *retained.Items[0].HashrateThs)
		require.Equal(t, power/1000, *retained.Items[0].PowerKw)
		snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Microsecond)
		module.State = sdk.ModuleUnknown
		module.ObservedAt = time.Time{}
		module.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: snapshot.ObservedAt}
		write()
		snapshot.ObservedAt = snapshot.ObservedAt.Add(time.Microsecond)
		module.State = sdk.ModuleHashing
		module.ObservedAt = now.Add(-time.Minute)
		module.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: now.Add(-time.Minute), HashrateHS: &hashrate, PowerW: &power}
		write()
		var state string
		var evidence time.Time
		require.NoError(t, db.QueryRowContext(ctx, "SELECT state,state_observed_at FROM container_module WHERE reported_id='replacement'").Scan(&state, &evidence))
		require.Equal(t, string(sdk.ModuleUnknown), state)
		require.Equal(t, now, evidence.UTC())
		result, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_TANK}}, scopes)
		require.NoError(t, err)
		require.Len(t, result.Items, 1)
		require.Zero(t, result.Items[0].HashrateReportingCount)
		require.Zero(t, result.Items[0].PowerReportingCount)
		require.Nil(t, result.Items[0].HashrateThs)
		require.Nil(t, result.Items[0].PowerKw)
	})

}

func TestMissingModuleIdentityOnlyClearsAcceptedEmptyObservations(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "anonymous-metrics", Name: "Anonymous metrics"})
	require.NoError(t, err)
	d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "anonymous-ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, SerialNumber: "controller", Model: "CT1", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.15", Port: "8080", UrlScheme: "http"}, OrgID: org, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: d.DeviceIdentifier}, d)
	require.NoError(t, err)
	require.NoError(t, sqlstores.NewSQLDeviceStore(db).InsertDevice(ctx, &d.Device, org, d.DeviceIdentifier))
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	hashrate, power, temp := 300e12, 3600.0, 62.0
	write := func(collected, source time.Time, identity string, empty bool) {
		t.Helper()
		metrics := &sdk.ContainerModuleMetrics{ObservedAt: source}
		if !empty {
			metrics.HashrateHS = &hashrate
			metrics.PowerW = &power
			metrics.TemperatureC = &temp
		}
		snapshot := &sdk.ContainerSnapshot{ObservedAt: collected, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, ReportedID: identity, State: sdk.ModuleHashing, ObservedAt: collected, Metrics: metrics}}}}}
		require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: collected, Container: snapshot}))
	}
	assertCurrent := func(empty bool, samples int) {
		t.Helper()
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM container_module_metrics mm JOIN container_module m ON m.id=mm.module_id WHERE m.reported_id='replacement'").Scan(&count))
		require.Equal(t, samples, count)
		var identity string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT reported_id FROM container_module WHERE retired_at IS NULL").Scan(&identity))
		require.Equal(t, "replacement", identity)
		reader := fleetinventory.NewService(sqlc.New(db))
		for _, view := range []inventorypb.InventoryView{inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, inventorypb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS} {
			result, err := reader.List(ctx, org, &inventorypb.ListInventoryRequest{View: view}, []fleetinventory.Scope{{OrgWide: true}})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			item := result.Items[0]
			if empty {
				require.Nil(t, item.HashrateThs)
				require.Nil(t, item.PowerKw)
				require.Nil(t, item.MinTemperatureC)
				require.Zero(t, item.HashrateReportingCount)
				require.Zero(t, item.PowerReportingCount)
			} else {
				require.NotNil(t, item.PowerKw)
				require.Equal(t, power/1000, *item.PowerKw)
				require.EqualValues(t, 1, item.HashrateReportingCount)
				require.EqualValues(t, 1, item.PowerReportingCount)
			}
		}
	}
	write(base, base, "original", false)
	write(base.Add(10*time.Second), base.Add(10*time.Second), "replacement", false)
	assertCurrent(false, 1)
	// Source time alone cannot authorize an anonymous poll captured before replacement.
	write(base.Add(8*time.Second), base.Add(12*time.Second), "", true)
	assertCurrent(false, 1)
	// Missing identity does not grant permission to attribute measurements.
	write(base.Add(20*time.Second), base.Add(20*time.Second), "", false)
	assertCurrent(false, 1)
	// An accepted empty observation still obeys source ordering.
	write(base.Add(30*time.Second), base.Add(5*time.Second), "", true)
	assertCurrent(false, 2)
	write(base.Add(40*time.Second), base.Add(40*time.Second), "", true)
	assertCurrent(true, 3)
	// Retrying is idempotent; late evidence for the replaced identity stays excluded.
	write(base.Add(40*time.Second), base.Add(40*time.Second), "", true)
	write(base.Add(9*time.Second), base.Add(9*time.Second), "original", false)
	assertCurrent(true, 3)
	// All Unicode blank identities use the same anonymous-metric policy.
	for i, blank := range []string{" \t\n", "\u00a0", "\u2003"} {
		at := base.Add(time.Duration(50+i*2) * time.Second)
		write(at, at, blank, false)
		assertCurrent(true, 3+i)
		write(at.Add(time.Second), at.Add(time.Second), blank, true)
		assertCurrent(true, 4+i)
	}
}
