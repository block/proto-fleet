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

// twoOrgFixture builds two orgs (A and B) with mirror-image building +
// rack + device topology under a shared zone label. Each filter branch
// runs against the fixture twice — once as orgA, once as orgB — and
// asserts each side only ever sees its own devices. If a future
// refactor drops the dcm.org_id = $orgID clause from any branch, this
// suite catches it.
type twoOrgFixture struct {
	orgA       int64
	orgB       int64
	buildingA  int64 // owned by orgA
	buildingB  int64 // owned by orgB
	devARacked string
	devBRacked string
	devANoBldg string
	devBNoBldg string
	devANoRack string
	devBNoRack string
}

func buildTwoOrgFixture(t *testing.T, ctx context.Context, tc *testutil.TestContext) twoOrgFixture {
	t.Helper()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	const sharedZone = "Room 2"

	userA := dbSvc.CreateSuperAdminUser()
	userB := dbSvc.CreateSuperAdminUser2()

	makeForOrg := func(orgID int64, label string) (int64, string, string, string) {
		t.Helper()
		site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site " + label})
		require.NoError(t, err)
		building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
			OrgID: orgID, SiteID: &site.ID, Name: "Bldg " + label,
		})
		require.NoError(t, err)

		// Rack with a building → exercises building_ids + scoped zone_keys.
		rackBldg, err := collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack "+label, "")
		require.NoError(t, err)
		require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
			OrgID: orgID, CollectionID: rackBldg.Id, Rows: 4, Columns: 8, Zone: sharedZone,
			SiteID: &site.ID, BuildingID: &building.ID,
		}))
		devRacked := dbSvc.CreateDevice(orgID, "proto")
		pairDevice(t, ctx, deviceStore, orgID, devRacked.ID)
		_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rackBldg.Id, []string{devRacked.ID})
		require.NoError(t, err)

		// Rack with NULL building_id → exercises include_no_building.
		rackNoBldg, err := collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack NoBldg "+label, "")
		require.NoError(t, err)
		require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
			OrgID: orgID, CollectionID: rackNoBldg.Id, Rows: 4, Columns: 8, Zone: sharedZone,
		}))
		devNoBldg := dbSvc.CreateDevice(orgID, "proto")
		pairDevice(t, ctx, deviceStore, orgID, devNoBldg.ID)
		_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rackNoBldg.Id, []string{devNoBldg.ID})
		require.NoError(t, err)

		// Device with no rack membership → exercises include_no_rack.
		devNoRack := dbSvc.CreateDevice(orgID, "proto")
		pairDevice(t, ctx, deviceStore, orgID, devNoRack.ID)

		return building.ID, devRacked.ID, devNoBldg.ID, devNoRack.ID
	}

	bldgA, devARacked, devANoBldg, devANoRack := makeForOrg(userA.OrganizationID, "A")
	bldgB, devBRacked, devBNoBldg, devBNoRack := makeForOrg(userB.OrganizationID, "B")

	return twoOrgFixture{
		orgA:       userA.OrganizationID,
		orgB:       userB.OrganizationID,
		buildingA:  bldgA,
		buildingB:  bldgB,
		devARacked: devARacked,
		devBRacked: devBRacked,
		devANoBldg: devANoBldg,
		devBNoBldg: devBNoBldg,
		devANoRack: devANoRack,
		devBNoRack: devBNoRack,
	}
}

// assertOrgIsolation runs the filter under both orgs and asserts each
// side sees only the expected own-org device and never the cross-org
// device. Centralizes the assertion shape so each branch test stays
// readable.
func assertOrgIsolation(
	t *testing.T,
	ctx context.Context,
	store *sqlstores.SQLDeviceStore,
	fx twoOrgFixture,
	filter *stores.MinerFilter,
	wantA, wantB, leakA, leakB string,
) {
	t.Helper()

	gotA, totalA := listDevices(t, ctx, store, fx.orgA, filter)
	assert.Equal(t, int64(1), totalA, "orgA must see exactly one device")
	assert.Contains(t, gotA, wantA)
	assert.NotContains(t, gotA, leakA, "orgA must NOT see orgB device")

	gotB, totalB := listDevices(t, ctx, store, fx.orgB, filter)
	assert.Equal(t, int64(1), totalB, "orgB must see exactly one device")
	assert.Contains(t, gotB, wantB)
	assert.NotContains(t, gotB, leakB, "orgB must NOT see orgA device")
}

// TestBuildingIDs_CrossOrgIsolation: passing orgB's building_id while
// querying as orgA must not surface orgB's devices. The SQL builder
// pairs building_id with dcm.org_id, so the explicit cross-org probe
// goes through the bulk validation layer in real callers — but at the
// SQL builder level, the org_id clause is what protects an org whose
// validation layer has been bypassed.
func TestBuildingIDs_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildTwoOrgFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// Each org filters by its own building. With the org_id clause
	// dropped, both queries would still return their own device because
	// the building_id is org-specific — but the cross-org assertion is
	// about the org_id guard fighting an attacker who knows another
	// org's building_id. We model that by querying orgA with orgB's
	// building, expecting an empty result.
	gotA, totalA := listDevices(t, ctx, deviceStore, fx.orgA, &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingB},
	})
	assert.Equal(t, int64(0), totalA, "orgA filtering by orgB's building must see nothing")
	assert.NotContains(t, gotA, fx.devBRacked)

	gotB, totalB := listDevices(t, ctx, deviceStore, fx.orgB, &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingA},
	})
	assert.Equal(t, int64(0), totalB, "orgB filtering by orgA's building must see nothing")
	assert.NotContains(t, gotB, fx.devARacked)

	// Own-org sanity: each side sees its own racked device when
	// filtering by its own building. `assertOrgIsolation` applies one
	// filter to both orgs, which doesn't work here — the filter value
	// itself (BuildingIDs) is org-specific. Inline both sides.
	gotAOwn, totalAOwn := listDevices(t, ctx, deviceStore, fx.orgA, &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingA},
	})
	assert.Equal(t, int64(1), totalAOwn)
	assert.Contains(t, gotAOwn, fx.devARacked)

	gotBOwn, totalBOwn := listDevices(t, ctx, deviceStore, fx.orgB, &stores.MinerFilter{
		BuildingIDs: []int64{fx.buildingB},
	})
	assert.Equal(t, int64(1), totalBOwn)
	assert.Contains(t, gotBOwn, fx.devBRacked)
}

// TestIncludeNoBuilding_CrossOrgIsolation: the IS NULL branch must
// scope to the caller's org. Without the org_id clause, orgA would see
// orgB's no-building rack devices.
func TestIncludeNoBuilding_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildTwoOrgFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	assertOrgIsolation(t, ctx, deviceStore, fx,
		&stores.MinerFilter{IncludeNoBuilding: true},
		fx.devANoBldg, fx.devBNoBldg, fx.devBNoBldg, fx.devANoBldg,
	)
}

// TestIncludeNoRack_CrossOrgIsolation: the NOT EXISTS branch must
// scope to the caller's org. Without the org_id clause, the subquery
// would treat orgB's rack memberships as evidence that orgA's
// no-rack device is, in fact, racked — which would hide it.
func TestIncludeNoRack_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildTwoOrgFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	assertOrgIsolation(t, ctx, deviceStore, fx,
		&stores.MinerFilter{IncludeNoRack: true},
		fx.devANoRack, fx.devBNoRack, fx.devBNoRack, fx.devANoRack,
	)
}

// TestZoneKeysScoped_CrossOrgIsolation: scoped zone_keys carry both
// building_id and zone. Even with the same zone label across orgs, the
// org_id clause must scope each query. This is the analog to
// TestZoneKeysWildcard_CrossOrgIsolation — both branches need
// coverage because they emit through different SQL shapes.
func TestZoneKeysScoped_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildTwoOrgFixture(t, ctx, tc)
	deviceStore := sqlstores.NewSQLDeviceStore(tc.ServiceProvider.DB)

	// orgA filtering by orgB's (building, zone) tuple: empty.
	gotA, totalA := listDevices(t, ctx, deviceStore, fx.orgA, &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: fx.buildingB, Zone: "Room 2"}},
	})
	assert.Equal(t, int64(0), totalA, "orgA must see nothing when filtering by orgB's scoped zone")
	assert.NotContains(t, gotA, fx.devBRacked)

	// orgB filtering by orgA's tuple: empty.
	gotB, totalB := listDevices(t, ctx, deviceStore, fx.orgB, &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: fx.buildingA, Zone: "Room 2"}},
	})
	assert.Equal(t, int64(0), totalB, "orgB must see nothing when filtering by orgA's scoped zone")
	assert.NotContains(t, gotB, fx.devARacked)

	// Own-side sanity: each org sees its own racked device when the
	// scoped tuple matches its own building.
	gotAOwn, totalAOwn := listDevices(t, ctx, deviceStore, fx.orgA, &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: fx.buildingA, Zone: "Room 2"}},
	})
	assert.Equal(t, int64(1), totalAOwn)
	assert.Contains(t, gotAOwn, fx.devARacked)

	gotBOwn, totalBOwn := listDevices(t, ctx, deviceStore, fx.orgB, &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: fx.buildingB, Zone: "Room 2"}},
	})
	assert.Equal(t, int64(1), totalBOwn)
	assert.Contains(t, gotBOwn, fx.devBRacked)
}
