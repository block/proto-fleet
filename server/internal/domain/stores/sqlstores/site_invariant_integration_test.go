package sqlstores_test

import (
	"context"
	"testing"

	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteSite_ClearsAllDeviceSitePointers asserts the invariant the
// miner-list query JOIN relies on: after DeleteSite soft-deletes a site,
// no live device.site_id points at it. The fleetmanagement snapshot
// builder LEFT JOINs site with site.deleted_at IS NULL; without this
// invariant a device could surface with SiteId populated but SiteLabel
// empty (because the join misses the soft-deleted site row). PR B's
// DeleteSite step 4 (UnassignDevicesFromSite) is what makes the
// invariant hold; this test catches any regression in that contract.
func TestDeleteSite_ClearsAllDeviceSitePointers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)

	// Create a site and reassign 2 of the 3 devices to it.
	site, err := siteStore.CreateSite(t.Context(), sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Austin",
	})
	require.NoError(t, err)

	_, err = siteStore.ReassignDevicesToSite(t.Context(), orgID, &site.ID, deviceIDs[:2])
	require.NoError(t, err)

	// Sanity: the two reassigned devices now point at the site.
	row := testContext.ServiceProvider.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id = $2`, orgID, site.ID)
	var beforeCount int
	require.NoError(t, row.Scan(&beforeCount))
	assert.Equal(t, 2, beforeCount, "test setup: 2 devices should point at the site")

	// Execute DeleteSite via the same tx wrapper production uses.
	err = transactor.RunInTx(t.Context(), func(txCtx context.Context) error {
		if err := siteStore.LockSiteForWrite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if err := siteStore.LockBuildingsBySiteForWrite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.UnassignRacksFromBuildingsBySite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.SoftDeleteBuildingsBySite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.UnassignRacksFromSite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.UnassignDevicesFromSite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := siteStore.SoftDeleteSite(txCtx, orgID, site.ID); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	// Invariant: no live device.site_id may reference a soft-deleted site.
	row = testContext.ServiceProvider.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM device d
		 WHERE d.org_id = $1
		   AND d.site_id IS NOT NULL
		   AND NOT EXISTS (
		     SELECT 1 FROM site s
		     WHERE s.id = d.site_id
		       AND s.org_id = d.org_id
		       AND s.deleted_at IS NULL
		   )`, orgID)
	var stalePointers int
	require.NoError(t, row.Scan(&stalePointers))
	assert.Equal(t, 0, stalePointers,
		"no live device.site_id should reference a soft-deleted site after DeleteSite")

	// And the formerly-assigned devices land in the Unassigned bucket.
	row = testContext.ServiceProvider.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM device WHERE org_id = $1 AND site_id IS NULL AND deleted_at IS NULL`, orgID)
	var unassignedCount int
	require.NoError(t, row.Scan(&unassignedCount))
	assert.GreaterOrEqual(t, unassignedCount, 3, "all 3 devices should now have site_id IS NULL")
}
