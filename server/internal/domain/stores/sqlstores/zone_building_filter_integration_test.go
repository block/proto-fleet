package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// zoneFixture builds the shared scenario for the building/zone filter
// suite:
//   - One site with two buildings (B1, B2), each with a rack whose zone
//     label is "Room 2" — this is the cross-building-collision case the
//     legacy flat zone filter conflated.
//   - Each rack carries 2 paired devices.
//   - A third rack with building_id IS NULL ("no-building rack") with one
//     paired device — exercises include_no_building.
//   - A standalone paired device with no rack membership at all —
//     exercises include_no_rack.
//
// Returns IDs the callers need to assert against.
type zoneFixture struct {
	orgID     int64
	siteID    int64
	buildingA int64
	buildingB int64
	devsInB1  []string // 2 devices in B1 / "Room 2"
	devsInB2  []string // 2 devices in B2 / "Room 2"
	devNoBldg string   // 1 device in a rack with NULL building_id
	devNoRack string   // 1 device with no rack membership
}

func buildZoneFixture(t *testing.T, ctx context.Context, tc *testutil.TestContext) zoneFixture {
	t.Helper()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site Main"})
	require.NoError(t, err)
	b1, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "Bldg 1",
	})
	require.NoError(t, err)
	b2, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "Bldg 2",
	})
	require.NoError(t, err)

	createRack := func(label, zone string, buildingID *int64) int64 {
		rack, err := collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, label, "")
		require.NoError(t, err)
		var siteIDPtr *int64
		if buildingID != nil {
			siteIDPtr = &site.ID
		}
		require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
			OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: zone,
			SiteID: siteIDPtr, BuildingID: buildingID,
		}))
		return rack.Id
	}

	pairDeviceLocal := func(deviceID string) {
		t.Helper()
		pairDevice(t, ctx, deviceStore, orgID, deviceID)
	}

	addDevices := func(rackID int64, count int) []string {
		ids := make([]string, 0, count)
		for range count {
			d := dbSvc.CreateDevice(orgID, "proto")
			pairDeviceLocal(d.ID)
			ids = append(ids, d.ID)
		}
		_, err := collectionStore.AddDevicesToCollection(ctx, orgID, rackID, ids)
		require.NoError(t, err)
		return ids
	}

	rackB1 := createRack("Rack B1", "Room 2", &b1.ID)
	rackB2 := createRack("Rack B2", "Room 2", &b2.ID)
	rackNoBldg := createRack("Rack NoBldg", "Room 2", nil)

	devsInB1 := addDevices(rackB1, 2)
	devsInB2 := addDevices(rackB2, 2)
	devNoBldgList := addDevices(rackNoBldg, 1)

	devNoRack := dbSvc.CreateDevice(orgID, "proto")
	pairDeviceLocal(devNoRack.ID)

	return zoneFixture{
		orgID:     orgID,
		siteID:    site.ID,
		buildingA: b1.ID,
		buildingB: b2.ID,
		devsInB1:  devsInB1,
		devsInB2:  devsInB2,
		devNoBldg: devNoBldgList[0],
		devNoRack: devNoRack.ID,
	}
}

// listDevices runs the snapshot query with a filter and returns the set of
// returned device identifiers and the total count. Wraps the boilerplate.
func listDevices(t *testing.T, ctx context.Context, store *sqlstores.SQLDeviceStore, orgID int64, filter *stores.MinerFilter) (map[string]struct{}, int64) {
	t.Helper()
	rows, _, total, err := store.ListMinerStateSnapshots(ctx, orgID, "", 100, filter, nil)
	require.NoError(t, err)
	got := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		got[r.DeviceIdentifier] = struct{}{}
	}
	return got, total
}

func TestZoneKeys_ScopedDistinguishesCrossBuildingCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// Scoped zone_keys to (B1, "Room 2") should match exactly the two
	// devices in B1 — never the two in B2 with the same zone label. This is
	// the marquee bug fix: the legacy org-wide zones filter would return all
	// four.
	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: fx.buildingA, Zone: "Room 2"}},
	}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)

	assert.Equal(t, int64(2), total)
	for _, d := range fx.devsInB1 {
		assert.Contains(t, got, d, "B1 device should match scoped zone_keys=(B1, Room 2)")
	}
	for _, d := range fx.devsInB2 {
		assert.NotContains(t, got, d, "B2 device must NOT match scoped zone_keys=(B1, Room 2)")
	}
}

func TestZoneKeys_WildcardMatchesAllBuildings(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// Wildcard {0, "Room 2"} preserves the legacy "any building" semantics.
	// The fixture has 5 devices in racks labeled "Room 2": 2 in B1, 2 in B2,
	// 1 in the no-building rack.
	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: 0, Zone: "Room 2"}},
	}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)

	assert.Equal(t, int64(5), total)
	for _, d := range append(append([]string{}, fx.devsInB1...), fx.devsInB2...) {
		assert.Contains(t, got, d)
	}
	assert.Contains(t, got, fx.devNoBldg, "wildcard zone matches the no-building rack too")
	assert.NotContains(t, got, fx.devNoRack, "no-rack device is excluded by any zone_keys filter")
}

func TestZoneKeys_MixedScopedAndWildcardUnion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// Scoped (B1, "Room 2") OR wildcard (0, "Nonexistent") — wildcard side
	// matches nothing, scoped side matches B1's two devices. Proves both
	// branches emit and OR cleanly.
	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{
			{BuildingID: fx.buildingA, Zone: "Room 2"},
			{BuildingID: 0, Zone: "Nonexistent Zone"},
		},
	}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(2), total)
	for _, d := range fx.devsInB1 {
		assert.Contains(t, got, d)
	}
}

func TestBuildingIDs_ScopesToBuilding(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	filter := &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingA},
	}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(2), total)
	for _, d := range fx.devsInB1 {
		assert.Contains(t, got, d)
	}
	for _, d := range fx.devsInB2 {
		assert.NotContains(t, got, d)
	}
}

func TestIncludeNoBuilding_SurfacesNullBuildingRack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// include_no_building alone — surfaces only the one device in the
	// no-building rack. Does NOT surface devices in B1 or B2, and does NOT
	// surface the no-rack device (use include_no_rack for that).
	filter := &stores.MinerFilter{IncludeNoBuilding: true}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(1), total)
	assert.Contains(t, got, fx.devNoBldg)
	assert.NotContains(t, got, fx.devNoRack)
}

func TestIncludeNoRack_SurfacesUnrackedDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// include_no_rack alone — surfaces only the one device with no
	// device_set_membership row.
	filter := &stores.MinerFilter{IncludeNoRack: true}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(1), total)
	assert.Contains(t, got, fx.devNoRack)
	assert.NotContains(t, got, fx.devNoBldg, "no-rack must NOT include racked-but-no-building")
}

func TestIncludeNoBuildingPlusBuildingIDs_OR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// building_ids = [B1] + include_no_building = true should union: B1's two
	// devices + the no-building rack's one device.
	filter := &stores.MinerFilter{
		BuildingIDs:       []int64{fx.buildingA},
		IncludeNoBuilding: true,
	}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(3), total)
	for _, d := range fx.devsInB1 {
		assert.Contains(t, got, d)
	}
	assert.Contains(t, got, fx.devNoBldg)
	assert.NotContains(t, got, fx.devNoRack)
}

func TestZoneKeysIntersectBuildingIDs_AND(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildZoneFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// building_ids=[B1] AND zone_keys=[{B2, "Room 2"}]: B1 devices fail the
	// zone predicate, B2 devices fail the building predicate → empty.
	filter := &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingA},
		ZoneKeys:    []stores.ZoneKey{{BuildingID: fx.buildingB, Zone: "Room 2"}},
	}
	_, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(0), total, "AND of disjoint scopes returns no devices")

	// Matching scope returns B1's two devices.
	filter = &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingA},
		ZoneKeys:    []stores.ZoneKey{{BuildingID: fx.buildingA, Zone: "Room 2"}},
	}
	got, total := listDevices(t, ctx, deviceStore, fx.orgID, filter)
	assert.Equal(t, int64(2), total)
	for _, d := range fx.devsInB1 {
		assert.Contains(t, got, d)
	}
}

// TestZoneKeysWildcard_CrossOrgIsolation guards the single-layer defense:
// wildcard zone_keys skip the parseFilter cross-org check, so the SQL
// builder's dcm.org_id = $orgID clause is the only org boundary. If a
// future refactor drops that clause, this test catches it.
func TestZoneKeysWildcard_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	userA := dbSvc.CreateSuperAdminUser()
	userB := dbSvc.CreateSuperAdminUser2()
	devA := dbSvc.CreateDevice(userA.OrganizationID, "proto")
	devB := dbSvc.CreateDevice(userB.OrganizationID, "proto")
	pairDevice(t, ctx, deviceStore, userA.OrganizationID, devA.ID)
	pairDevice(t, ctx, deviceStore, userB.OrganizationID, devB.ID)

	const sharedZone = "shared-zone"
	for _, who := range []struct {
		orgID    int64
		deviceID string
		label    string
	}{
		{userA.OrganizationID, devA.ID, "Rack A"},
		{userB.OrganizationID, devB.ID, "Rack B"},
	} {
		rack, err := collectionStore.CreateCollection(ctx, who.orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, who.label, "")
		require.NoError(t, err)
		require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
			OrgID: who.orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: sharedZone,
		}))
		_, err = collectionStore.AddDevicesToCollection(ctx, who.orgID, rack.Id, []string{who.deviceID})
		require.NoError(t, err)
	}

	wildcard := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: 0, Zone: sharedZone}},
	}
	gotA, totalA := listDevices(t, ctx, deviceStore, userA.OrganizationID, wildcard)
	assert.Equal(t, int64(1), totalA)
	assert.Contains(t, gotA, devA.ID)
	assert.NotContains(t, gotA, devB.ID)

	gotB, totalB := listDevices(t, ctx, deviceStore, userB.OrganizationID, wildcard)
	assert.Equal(t, int64(1), totalB)
	assert.Contains(t, gotB, devB.ID)
	assert.NotContains(t, gotB, devA.ID)
}
