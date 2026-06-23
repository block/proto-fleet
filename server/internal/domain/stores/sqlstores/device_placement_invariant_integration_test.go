package sqlstores_test

import (
	"context"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/buildings"
	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	sqlstoresinterfaces "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/stretchr/testify/require"
)

// devicePlacementInvariantViolations runs the consistency query against the
// raw *sql.DB and returns the offending device identifiers (empty = holds).
//
// The invariant has two clauses:
//
//  1. Direct building↔site: if device.building_id is set, the building's
//     site_id must equal device.site_id (NULL == NULL counts as equal).
//  2. Rack lockstep: if the device is a member of ANY live rack,
//     device.site_id and device.building_id must equal that rack's
//     site_id / building_id. This includes a fully-unassigned rack (no
//     site, no building): a rack always dictates its members' placement,
//     so a member of a site-less rack must itself be site-less. (A miner
//     with a site sitting in a site-less rack is exactly the membership-
//     tree divergence this guards against.)
//
// The query is operation-agnostic: it doesn't know which RPC ran, only
// whether the result is consistent — so it catches any reparent path that
// updates one column's cascade but forgets the other, today or in a
// future refactor (see #495 / #492).
func devicePlacementInvariantViolations(ctx context.Context, t *testing.T, tc *testutil.TestContext, orgID int64) []string {
	t.Helper()
	rows, err := tc.ServiceProvider.DB.QueryContext(ctx, `
		SELECT d.device_identifier
		FROM device d
		LEFT JOIN building b
		  ON b.id = d.building_id AND b.org_id = d.org_id
		LEFT JOIN device_set_membership dsm
		  ON dsm.device_id = d.id AND dsm.org_id = d.org_id AND dsm.device_set_type = 'rack'
		LEFT JOIN device_set ds
		  ON ds.id = dsm.device_set_id AND ds.deleted_at IS NULL
		LEFT JOIN device_set_rack dsr
		  ON dsr.device_set_id = dsm.device_set_id AND dsr.org_id = dsm.org_id
		WHERE d.org_id = $1
		  AND d.deleted_at IS NULL
		  AND (
		    -- (1) building set but its site disagrees with the device's site
		    (d.building_id IS NOT NULL AND b.site_id IS DISTINCT FROM d.site_id)
		    -- (2) device in ANY live rack whose site/building diverges from
		    --     the rack's. A rack always dictates member placement, so a
		    --     site-less rack requires site-less members too.
		    OR (ds.id IS NOT NULL
		        AND (d.site_id IS DISTINCT FROM dsr.site_id
		             OR d.building_id IS DISTINCT FROM dsr.building_id))
		  )`, orgID)
	require.NoError(t, err)
	defer rows.Close()

	var offenders []string
	for rows.Next() {
		var ident string
		require.NoError(t, rows.Scan(&ident))
		offenders = append(offenders, ident)
	}
	require.NoError(t, rows.Err())
	return offenders
}

// TestDevicePlacementInvariant_HoldsAcrossReparentPaths drives every
// reparent service path that writes device.site_id / device.building_id
// and asserts, after each one, that no live device is left in an
// inconsistent site/building state.
//
// Unlike the service-layer mock tests (which verify a specific cascade
// was *called*), this asserts the *outcome* against real SQL, so it
// catches a path that wires a cascade wrong or forgets one entirely —
// the exact class of bug surfaced repeatedly in review. It also survives
// the planned refactors (#495 choke-point helper, #492 server-driven
// preview): those change how the invariant is maintained, not the
// invariant itself, so this stays the regression guard for both.
func TestDevicePlacementInvariant_HoldsAcrossReparentPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	ctx := t.Context()

	siteStore := sqlstores.NewSQLSiteStore(tc.ServiceProvider.DB)
	buildingStore := sqlstores.NewSQLBuildingStore(tc.ServiceProvider.DB)
	collectionStore := sqlstores.NewSQLCollectionStore(tc.ServiceProvider.DB)
	transactor := sqlstores.NewSQLTransactor(tc.ServiceProvider.DB)

	// Services wired against the real stores. deviceQueryer / telemetry /
	// activity are stats- and audit-only and unused by the reparent
	// writes, so nil is fine. The collection resolver is never reached
	// because AssignDevicesToRack takes an explicit identifier list.
	buildingsSvc := buildings.NewService(buildingStore, siteStore, collectionStore, nil, nil, transactor, nil)
	sitesSvc := sites.NewService(siteStore, buildingStore, collectionStore, nil, nil, transactor, nil)
	collectionSvc := collection.NewService(
		collectionStore, nil, siteStore, buildingStore, transactor,
		func(context.Context, *commonpb.DeviceSelector, int64) ([]string, error) { return nil, nil },
		nil, nil,
	)

	assertHolds := func(label string) {
		t.Helper()
		offenders := devicePlacementInvariantViolations(ctx, t, tc, orgID)
		require.Empty(t, offenders, "device placement invariant violated after %s: %v", label, offenders)
	}

	// --- Fixture: two sites, a building in each, one site-less building,
	// two racks, and a pool of devices.
	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)

	bldgA, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{OrgID: orgID, SiteID: &siteA.ID, Name: "Bldg A1"})
	require.NoError(t, err)
	bldgB, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{OrgID: orgID, SiteID: &siteB.ID, Name: "Bldg B1"})
	require.NoError(t, err)
	sitelessBldg, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{OrgID: orgID, Name: "Unassigned Bldg"})
	require.NoError(t, err)

	rackA, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: rackA.Id, Rows: 4, Columns: 8, SiteID: &siteA.ID, BuildingID: &bldgA.ID,
	}))

	// A fully-unassigned rack (no site, no building) — the staging case
	// the strict invariant covers: its members must be site-less too.
	sitelessRack, err := collectionStore.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack C", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, sqlstoresinterfaces.CreateRackExtensionParams{
		OrgID: orgID, CollectionID: sitelessRack.Id, Rows: 4, Columns: 8,
	}))

	devices := tc.DatabaseService.CreateTestMiners(orgID, 4, "https://172.17.0.1:80")
	require.Len(t, devices, 4)

	assertHolds("fixture setup")

	// --- Drive each reparent path, asserting the invariant after each.

	// 1. AssignDevicesToRack: devices inherit rackA's site + building.
	_, err = collectionSvc.AssignDevicesToRack(ctx, collection.AssignDevicesToRackParams{
		OrgID: orgID, TargetRackID: &rackA.Id, DeviceIdentifiers: devices,
	})
	require.NoError(t, err)
	assertHolds("AssignDevicesToRack")

	// 1b. AssignDevicesToRack into a site-less rack: device[3] currently has
	//     rackA's site/building, so joining the unassigned rack must strip
	//     it (force_clear_conflicting_site) to keep the membership tree
	//     consistent — a sited miner can't live in a site-less rack.
	_, err = collectionSvc.AssignDevicesToRack(ctx, collection.AssignDevicesToRackParams{
		OrgID: orgID, TargetRackID: &sitelessRack.Id, DeviceIdentifiers: devices[3:4],
		ForceClearConflictingSite: true,
	})
	require.NoError(t, err)
	assertHolds("AssignDevicesToRack (site-less rack, force-strip)")

	// 2. AssignDevicesToBuilding: move two devices directly into bldgB
	//    (force-clear their rackA membership, which is at a different site).
	_, _, err = buildingsSvc.AssignDevicesToBuilding(ctx, buildingsmodels.AssignDevicesToBuildingParams{
		OrgID: orgID, TargetBuildingID: &bldgB.ID, DeviceIdentifiers: devices[:2],
		ForceClearConflictingRackMembership: true,
	})
	require.NoError(t, err)
	assertHolds("AssignDevicesToBuilding (cross-site, force-clear)")

	// 3. AssignDevicesToBuilding into the site-less building: device.site_id
	//    must cascade to NULL to stay in lockstep with the unassigned building.
	_, _, err = buildingsSvc.AssignDevicesToBuilding(ctx, buildingsmodels.AssignDevicesToBuildingParams{
		OrgID: orgID, TargetBuildingID: &sitelessBldg.ID, DeviceIdentifiers: devices[:1],
		ForceClearConflictingRackMembership: true,
	})
	require.NoError(t, err)
	assertHolds("AssignDevicesToBuilding (site-less building)")

	// 4. AssignDevicesToSite: a direct site move must clear any direct-FK
	//    building that now points at the wrong site.
	_, _, err = sitesSvc.AssignDevicesToSite(ctx, sitesmodels.AssignDevicesToSiteParams{
		OrgID: orgID, TargetSiteID: &siteA.ID, DeviceIdentifiers: devices[:2],
		ForceClearConflictingRackMembership: true,
	})
	require.NoError(t, err)
	assertHolds("AssignDevicesToSite")

	// 5. AssignRacksToBuilding: move rackA into bldgB; members follow.
	_, err = buildingsSvc.AssignRacksToBuilding(ctx, buildingsmodels.AssignRacksToBuildingParams{
		OrgID: orgID, TargetBuildingID: &bldgB.ID, Racks: []buildingsmodels.RackPlacementParam{{RackID: rackA.Id}},
	})
	require.NoError(t, err)
	assertHolds("AssignRacksToBuilding")

	// 6. AssignRacksToSite: cross-site rack move clears rack.building_id and
	//    cascades both columns for members.
	_, err = sitesSvc.AssignRacksToSite(ctx, sitesmodels.AssignRacksToSiteParams{
		OrgID: orgID, TargetSiteID: &siteA.ID, RackIDs: []int64{rackA.Id},
	})
	require.NoError(t, err)
	assertHolds("AssignRacksToSite")

	// 7. AssignBuildingsToSite: move bldgB to site A; rack + direct-FK
	//    devices under it re-stamp.
	_, err = sitesSvc.AssignBuildingsToSite(ctx, sitesmodels.AssignBuildingsToSiteParams{
		OrgID: orgID, BuildingIDs: []int64{bldgB.ID}, TargetSiteID: &siteA.ID,
	})
	require.NoError(t, err)
	assertHolds("AssignBuildingsToSite")

	// 8. DeleteBuilding: soft-delete must clear direct-FK building pointers.
	_, err = buildingsSvc.DeleteBuilding(ctx, orgID, sitelessBldg.ID)
	require.NoError(t, err)
	assertHolds("DeleteBuilding")

	// 9. DeleteSite: cascade clears device site + building pointers under it.
	_, err = sitesSvc.DeleteSite(ctx, orgID, siteB.ID)
	require.NoError(t, err)
	assertHolds("DeleteSite")
}
