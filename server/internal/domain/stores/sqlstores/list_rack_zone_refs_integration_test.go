package sqlstores_test

import (
	"strconv"
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

// TestListRackZoneRefs_ReturnsCompositeEntries verifies the new
// device_set.v1.ListRackZones path: ListRackZoneRefs returns
// (building_id, building_label, site_id, site_label, zone) tuples
// joined through building + site, sorted site_label, building_label,
// zone. The deprecated collection.v1 ListRackZones flat-string shape
// is left alone — device_set.v1 bypasses it via this method.
func TestListRackZoneRefs_ReturnsCompositeEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Alpha Site"})
	require.NoError(t, err)
	b1, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "B1",
	})
	require.NoError(t, err)
	b2, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "B2",
	})
	require.NoError(t, err)

	createRack := func(label, zone string, buildingID *int64) {
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
	}

	createRack("Rack 1", "Room 2", &b1.ID)
	createRack("Rack 2", "Room 2", &b2.ID) // same zone label, different building
	createRack("Rack 3", "Cold Aisle", &b1.ID)
	createRack("Rack Legacy", "Legacy Zone", nil) // building_id IS NULL surfaces with building_id=0

	refs, err := collectionStore.ListRackZoneRefs(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, refs, 4)

	// Convert to comparable form keyed by (building_id, zone) — sort order
	// also matters but is asserted separately below.
	byKey := make(map[string]stores.ZoneRefRow, len(refs))
	for _, r := range refs {
		key := zoneRefKey(r)
		byKey[key] = r
	}

	// B1 / Room 2
	r1 := byKey[zoneRefKeyOf(b1.ID, "Room 2")]
	assert.Equal(t, b1.ID, r1.BuildingID)
	assert.Equal(t, "B1", r1.BuildingLabel)
	assert.Equal(t, site.ID, r1.SiteID)
	assert.Equal(t, "Alpha Site", r1.SiteLabel)
	assert.Equal(t, "Room 2", r1.Zone)

	// B2 / Room 2 — separate row, proves the building-scoped distinction.
	r2 := byKey[zoneRefKeyOf(b2.ID, "Room 2")]
	assert.Equal(t, b2.ID, r2.BuildingID)
	assert.Equal(t, "B2", r2.BuildingLabel)
	assert.Equal(t, "Room 2", r2.Zone)

	// B1 / Cold Aisle
	r3 := byKey[zoneRefKeyOf(b1.ID, "Cold Aisle")]
	assert.Equal(t, b1.ID, r3.BuildingID)
	assert.Equal(t, "Cold Aisle", r3.Zone)

	// Legacy rack with NULL building_id surfaces as building_id=0 + empty
	// building/site labels.
	rL := byKey[zoneRefKeyOf(0, "Legacy Zone")]
	assert.Equal(t, int64(0), rL.BuildingID)
	assert.Empty(t, rL.BuildingLabel)
	assert.Equal(t, int64(0), rL.SiteID)
	assert.Empty(t, rL.SiteLabel)
	assert.Equal(t, "Legacy Zone", rL.Zone)
}

// TestListRackZoneRefs_SortOrder asserts the result is ordered by
// (site_label, building_label, zone). UI dropdown grouping depends on
// this — the FE renders the flat list assuming the server pre-sorted.
func TestListRackZoneRefs_SortOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	// Two sites, two buildings each, two zones per building. Names chosen
	// so alphabetical order is unambiguous.
	siteAlpha, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Alpha"})
	require.NoError(t, err)
	siteBravo, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Bravo"})
	require.NoError(t, err)

	addRack := func(siteID int64, buildingName, zone string) {
		bld, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
			OrgID: orgID, SiteID: &siteID, Name: buildingName,
		})
		require.NoError(t, err)
		rack, err := collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, buildingName+"/"+zone, "")
		require.NoError(t, err)
		require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
			OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: zone,
			SiteID: &siteID, BuildingID: &bld.ID,
		}))
	}

	addRack(siteBravo.ID, "Charlie", "Zeta")
	addRack(siteAlpha.ID, "Delta", "Beta")
	addRack(siteAlpha.ID, "Charlie", "Alpha")
	addRack(siteBravo.ID, "Alpha", "Beta")

	refs, err := collectionStore.ListRackZoneRefs(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, refs, 4)

	// Expected order: (Alpha, Charlie, Alpha), (Alpha, Delta, Beta),
	// (Bravo, Alpha, Beta), (Bravo, Charlie, Zeta).
	expected := []struct {
		site, building, zone string
	}{
		{"Alpha", "Charlie", "Alpha"},
		{"Alpha", "Delta", "Beta"},
		{"Bravo", "Alpha", "Beta"},
		{"Bravo", "Charlie", "Zeta"},
	}
	for i, want := range expected {
		assert.Equal(t, want.site, refs[i].SiteLabel, "row %d site_label", i)
		assert.Equal(t, want.building, refs[i].BuildingLabel, "row %d building_label", i)
		assert.Equal(t, want.zone, refs[i].Zone, "row %d zone", i)
	}
}

// TestListRackZoneRefs_CrossOrgIsolation guards the org boundary:
// org A must never see zones from org B's racks.
func TestListRackZoneRefs_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	userA := dbSvc.CreateSuperAdminUser()
	userB := dbSvc.CreateSuperAdminUser2()

	for _, who := range []struct {
		orgID int64
		zone  string
	}{
		{userA.OrganizationID, "Org-A-Zone"},
		{userB.OrganizationID, "Org-B-Zone"},
	} {
		site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: who.orgID, Name: "Site"})
		require.NoError(t, err)
		bld, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
			OrgID: who.orgID, SiteID: &site.ID, Name: "Bldg",
		})
		require.NoError(t, err)
		rack, err := collectionStore.CreateCollection(ctx, who.orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
		require.NoError(t, err)
		require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
			OrgID: who.orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: who.zone,
			SiteID: &site.ID, BuildingID: &bld.ID,
		}))
	}

	refsA, err := collectionStore.ListRackZoneRefs(ctx, userA.OrganizationID)
	require.NoError(t, err)
	require.Len(t, refsA, 1)
	assert.Equal(t, "Org-A-Zone", refsA[0].Zone)

	refsB, err := collectionStore.ListRackZoneRefs(ctx, userB.OrganizationID)
	require.NoError(t, err)
	require.Len(t, refsB, 1)
	assert.Equal(t, "Org-B-Zone", refsB[0].Zone)
}

// TestListRackZoneRefs_ExcludesSoftDeletedRacks confirms the query
// honors device_set.deleted_at — a soft-deleted rack must not surface
// in the dropdown, otherwise operators see stale zone options.
func TestListRackZoneRefs_ExcludesSoftDeletedRacks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site"})
	require.NoError(t, err)
	bld, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "Bldg",
	})
	require.NoError(t, err)

	live, err := collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Live Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: live.Id, Rows: 4, Columns: 8, Zone: "Live Zone",
		SiteID: &site.ID, BuildingID: &bld.ID,
	}))

	dead, err := collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Doomed Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: dead.Id, Rows: 4, Columns: 8, Zone: "Doomed Zone",
		SiteID: &site.ID, BuildingID: &bld.ID,
	}))
	_, err = collectionStore.SoftDeleteCollection(ctx, orgID, dead.Id)
	require.NoError(t, err)

	refs, err := collectionStore.ListRackZoneRefs(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "Live Zone", refs[0].Zone, "soft-deleted rack must not surface in zone refs")
}

// TestListRackZoneRefs_EmptyOrg covers an org with no racks at all.
// The dropdown rendering needs an empty slice (not nil, not an error)
// so the UI shows an empty state cleanly. This was a regression risk
// when the store originally short-circuited on len==0.
func TestListRackZoneRefs_EmptyOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	collectionStore := sqlstores.NewSQLCollectionStore(db)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	refs, err := collectionStore.ListRackZoneRefs(ctx, orgID)
	require.NoError(t, err)
	assert.Empty(t, refs, "org with no racks must return empty zone refs")
}

func zoneRefKey(r stores.ZoneRefRow) string {
	return zoneRefKeyOf(r.BuildingID, r.Zone)
}

func zoneRefKeyOf(buildingID int64, zone string) string {
	return strconv.FormatInt(buildingID, 10) + "|" + zone
}
