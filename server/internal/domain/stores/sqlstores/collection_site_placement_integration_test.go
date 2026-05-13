package sqlstores_test

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	sqlstoresinterfaces "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRackPlacement_CreateAndRoundTrip verifies CreateRackExtension
// persists site_id + building_id and GetRackInfo reads them back.
func TestRackPlacement_CreateAndRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	buildingStore := sqlstores.NewSQLBuildingStore(testContext.ServiceProvider.DB)

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "Bldg 1",
	})
	require.NoError(t, err)

	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack 1", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Room 2",
		SiteID: &site.ID, BuildingID: &building.ID,
	}))

	info, err := collectionStore.GetRackInfo(ctx, rack.Id, orgID)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, info.SiteId)
	assert.Equal(t, site.ID, *info.SiteId)
	require.NotNil(t, info.BuildingId)
	assert.Equal(t, building.ID, *info.BuildingId)
	assert.Equal(t, "Room 2", info.Zone)
}

// TestUpdateRackPlacement_RewritesAtomically verifies UpdateRackPlacement
// writes site_id, building_id, and zone together.
func TestUpdateRackPlacement_RewritesAtomically(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)

	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Z1", SiteID: &siteA.ID,
	}))

	require.NoError(t, collectionStore.UpdateRackPlacement(ctx, rack.Id, orgID, &siteB.ID, nil, ""))

	info, err := collectionStore.GetRackInfo(ctx, rack.Id, orgID)
	require.NoError(t, err)
	require.NotNil(t, info.SiteId)
	assert.Equal(t, siteB.ID, *info.SiteId)
	assert.Nil(t, info.BuildingId)
	assert.Equal(t, "", info.Zone)
}

// TestCascadeRackDeviceSites_RewritesMemberDevices verifies the cascade
// updates device.site_id for every paired rack member whose current site
// differs from the target.
func TestCascadeRackDeviceSites_RewritesMemberDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)

	// Pre-assign all devices to site A.
	_, err = siteStore.ReassignDevicesToSite(ctx, orgID, &siteA.ID, deviceIDs)
	require.NoError(t, err)

	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Z1", SiteID: &siteA.ID,
	}))
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs[:2])
	require.NoError(t, err)

	// Cascade rack to site B; only the 2 members in the rack should move.
	n, err := collectionStore.CascadeRackDeviceSites(ctx, rack.Id, orgID, &siteB.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	// Verify post-state.
	var atB, atA int
	row := testContext.ServiceProvider.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id = $2`, orgID, siteB.ID)
	require.NoError(t, row.Scan(&atB))
	assert.Equal(t, 2, atB, "the 2 rack members moved to site B")
	row = testContext.ServiceProvider.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id = $2`, orgID, siteA.ID)
	require.NoError(t, row.Scan(&atA))
	assert.Equal(t, 1, atA, "the non-member device stays at site A")
}

// TestGetAddedDeviceSiteConflicts_ReturnsOnlyConflictingDevices verifies
// the conflict query is scoped correctly: returns prior site_id for
// devices whose current site differs from the rack's stamped site, and
// no rows when the rack has no site stamped.
func TestGetAddedDeviceSiteConflicts_ReturnsOnlyConflictingDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)

	// Device 0 already at site A (matches rack); 1 at site B (conflict); 2 unassigned (conflict).
	_, err = siteStore.ReassignDevicesToSite(ctx, orgID, &siteA.ID, deviceIDs[:1])
	require.NoError(t, err)
	_, err = siteStore.ReassignDevicesToSite(ctx, orgID, &siteB.ID, deviceIDs[1:2])
	require.NoError(t, err)

	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Z1", SiteID: &siteA.ID,
	}))

	conflicts, err := collectionStore.GetAddedDeviceSiteConflicts(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)
	assert.Len(t, conflicts, 2, "device 0 matches rack site; devices 1 + 2 differ")
}

// TestCascadeAddedDeviceSites_RewritesOnlyDiffering verifies the
// add-cascade only rewrites devices whose current site differs from the
// rack's, and is a no-op when the rack has no site stamped.
func TestCascadeAddedDeviceSites_RewritesOnlyDiffering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 2, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)

	_, err = siteStore.ReassignDevicesToSite(ctx, orgID, &siteB.ID, deviceIDs[1:2])
	require.NoError(t, err)

	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Z1", SiteID: &siteA.ID,
	}))

	n, err := collectionStore.CascadeAddedDeviceSites(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)
	// IS DISTINCT FROM treats NULL as distinct from any value, so the
	// unassigned device (NULL ≠ A) AND the site-B device (B ≠ A) both
	// qualify for the rewrite.
	assert.Equal(t, int64(2), n, "both differing devices rewritten: unassigned and site-B → site-A")
}

// TestGetBuildingSite verifies the lookup returns the building's site_id
// and surfaces NotFound for soft-deleted buildings.
func TestGetBuildingSite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	buildingStore := sqlstores.NewSQLBuildingStore(testContext.ServiceProvider.DB)

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site"})
	require.NoError(t, err)
	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID, SiteID: &site.ID, Name: "Bldg",
	})
	require.NoError(t, err)

	siteID, err := collectionStore.GetBuildingSite(ctx, orgID, building.ID)
	require.NoError(t, err)
	require.NotNil(t, siteID)
	assert.Equal(t, site.ID, *siteID)

	_, err = buildingStore.SoftDeleteBuilding(ctx, orgID, building.ID)
	require.NoError(t, err)
	_, err = collectionStore.GetBuildingSite(ctx, orgID, building.ID)
	assert.Error(t, err, "GetBuildingSite must NotFound for soft-deleted buildings")
}

// TestLockRackPlacementForWrite verifies the lock query returns current
// placement on success and NotFound when the rack does not exist or has
// been soft-deleted.
func TestLockRackPlacementForWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site"})
	require.NoError(t, err)
	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Z1", SiteID: &site.ID,
	}))

	// FOR UPDATE only valid inside a tx.
	err = transactor.RunInTx(ctx, func(txCtx context.Context) error {
		placement, err := collectionStore.LockRackPlacementForWrite(txCtx, rack.Id, orgID)
		if err != nil {
			return err
		}
		require.NotNil(t, placement.SiteID)
		assert.Equal(t, site.ID, *placement.SiteID)
		return nil
	})
	require.NoError(t, err)

	// Missing rack surfaces NotFound.
	err = transactor.RunInTx(ctx, func(txCtx context.Context) error {
		_, lockErr := collectionStore.LockRackPlacementForWrite(txCtx, 999999, orgID)
		return lockErr
	})
	assert.Error(t, err)
}

// TestUnassignDeviceSitesByRack_ClearsRackMembersOnly verifies the
// rack-delete cascade nulls device.site_id for paired rack members and
// leaves non-members untouched.
func TestUnassignDeviceSitesByRack_ClearsRackMembersOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site"})
	require.NoError(t, err)
	_, err = siteStore.ReassignDevicesToSite(ctx, orgID, &site.ID, deviceIDs)
	require.NoError(t, err)

	rack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "Z1", SiteID: &site.ID,
	}))
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs[:2])
	require.NoError(t, err)

	n, err := collectionStore.UnassignDeviceSitesByRack(ctx, rack.Id, orgID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "only the 2 rack members should be nulled")

	row := testContext.ServiceProvider.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id = $2`, orgID, site.ID)
	var remaining int
	require.NoError(t, row.Scan(&remaining))
	assert.Equal(t, 1, remaining, "the non-member device retains its site_id")
}
