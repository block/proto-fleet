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

// twoOrgFixture has two organizations, A and B, with the same topology
// and the same zone label. Each organization has:
//   - one device in a rack with a building,
//   - one device in a rack with no building,
//   - one device with no rack,
//   - two devices whose only rack membership row has the org_id of the
//     other organization and points to a rack of that organization (one
//     rack with a building, one without).
//
// The base query limits device rows to the caller's organization. The
// last two devices make the dcm.org_id = $orgID clause of each
// rack-membership predicate change a result, so a predicate without the
// clause fails a subtest.
type twoOrgFixture struct {
	orgA              int64
	orgB              int64
	buildingA         int64 // owned by orgA
	buildingB         int64 // owned by orgB
	devARacked        string
	devBRacked        string
	devANoBldg        string
	devBNoBldg        string
	devANoRack        string
	devBNoRack        string
	devAForeignRack   string // orgA device, orgB membership row, rack in buildingB
	devBForeignRack   string // orgB device, orgA membership row, rack in buildingA
	devAForeignNoBldg string // orgA device, orgB membership row, rack with no building
	devBForeignNoBldg string // orgB device, orgA membership row, rack with no building
}

const twoOrgSharedZone = "Room 2"

func buildTwoOrgFixture(t *testing.T, ctx context.Context, dbSvc *testutil.DatabaseService) twoOrgFixture {
	t.Helper()
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)
	buildingStore := sqlstores.NewSQLBuildingStore(db)

	userA := dbSvc.CreateSuperAdminUser()
	userB := dbSvc.CreateSuperAdminUser2()

	type orgTopology struct {
		building   int64
		rackBldg   int64
		rackNoBldg int64
		devRacked  string
		devNoBldg  string
		devNoRack  string
	}

	makeForOrg := func(orgID int64, label string) orgTopology {
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
			OrgID: orgID, CollectionID: rackBldg.Id, Rows: 4, Columns: 8, Zone: twoOrgSharedZone,
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
			OrgID: orgID, CollectionID: rackNoBldg.Id, Rows: 4, Columns: 8, Zone: twoOrgSharedZone,
		}))
		devNoBldg := dbSvc.CreateDevice(orgID, "proto")
		pairDevice(t, ctx, deviceStore, orgID, devNoBldg.ID)
		_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rackNoBldg.Id, []string{devNoBldg.ID})
		require.NoError(t, err)

		// Device with no rack membership → exercises include_no_rack.
		devNoRack := dbSvc.CreateDevice(orgID, "proto")
		pairDevice(t, ctx, deviceStore, orgID, devNoRack.ID)

		return orgTopology{
			building:   building.ID,
			rackBldg:   rackBldg.Id,
			rackNoBldg: rackNoBldg.Id,
			devRacked:  devRacked.ID,
			devNoBldg:  devNoBldg.ID,
			devNoRack:  devNoRack.ID,
		}
	}

	// addForeignMember adds a device of deviceOrg. Its only rack membership
	// row has the org_id of rackOrg and points to a rack of rackOrg. The
	// store API cannot write this row, so the helper writes it directly.
	addForeignMember := func(deviceOrg, rackOrg, rackID int64) string {
		t.Helper()
		dev := dbSvc.CreateDevice(deviceOrg, "proto")
		pairDevice(t, ctx, deviceStore, deviceOrg, dev.ID)
		_, err := db.ExecContext(ctx, `
			INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier)
			VALUES ($1, $2, 'rack', $3, $4)`, rackOrg, rackID, dev.DatabaseID, dev.ID)
		require.NoError(t, err)
		return dev.ID
	}

	a := makeForOrg(userA.OrganizationID, "A")
	b := makeForOrg(userB.OrganizationID, "B")

	return twoOrgFixture{
		orgA:              userA.OrganizationID,
		orgB:              userB.OrganizationID,
		buildingA:         a.building,
		buildingB:         b.building,
		devARacked:        a.devRacked,
		devBRacked:        b.devRacked,
		devANoBldg:        a.devNoBldg,
		devBNoBldg:        b.devNoBldg,
		devANoRack:        a.devNoRack,
		devBNoRack:        b.devNoRack,
		devAForeignRack:   addForeignMember(userA.OrganizationID, userB.OrganizationID, b.rackBldg),
		devBForeignRack:   addForeignMember(userB.OrganizationID, userA.OrganizationID, a.rackBldg),
		devAForeignNoBldg: addForeignMember(userA.OrganizationID, userB.OrganizationID, b.rackNoBldg),
		devBForeignNoBldg: addForeignMember(userB.OrganizationID, userA.OrganizationID, a.rackNoBldg),
	}
}

// TestMinerFilter_CrossOrgIsolation runs each rack-membership filter as
// organization A and as organization B. Each side must get exactly its own
// devices. The subtests only query the fixture, so they share one database.
//
// Each subtest fails when the dcm.org_id = $orgID clause of its predicate
// is absent, because the fixture has membership rows of the other
// organization on the caller's devices.
func TestMinerFilter_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	dbSvc := testutil.NewDatabaseService(t, nil)
	fx := buildTwoOrgFixture(t, t.Context(), dbSvc)
	deviceStore := sqlstores.NewSQLDeviceStore(dbSvc.DB)

	assertDevices := func(t *testing.T, orgID int64, filter *stores.MinerFilter, msg string, want ...string) {
		t.Helper()
		got, total := listDevices(t, t.Context(), deviceStore, orgID, filter)
		wantSet := make(map[string]struct{}, len(want))
		for _, id := range want {
			wantSet[id] = struct{}{}
		}
		assert.Equal(t, wantSet, got, msg)
		assert.Equal(t, int64(len(want)), total, msg)
	}

	// A building filter with the building of the other organization models
	// a caller that got past the validation layer. The rack of that building
	// has a membership row for one of the caller's devices.
	t.Run("BuildingIDs", func(t *testing.T) {
		assertDevices(t, fx.orgA, &stores.MinerFilter{BuildingIDs: []int64{fx.buildingB}},
			"orgA filtering by orgB's building must see nothing")
		assertDevices(t, fx.orgB, &stores.MinerFilter{BuildingIDs: []int64{fx.buildingA}},
			"orgB filtering by orgA's building must see nothing")

		assertDevices(t, fx.orgA, &stores.MinerFilter{BuildingIDs: []int64{fx.buildingA}},
			"orgA filtering by its own building", fx.devARacked)
		assertDevices(t, fx.orgB, &stores.MinerFilter{BuildingIDs: []int64{fx.buildingB}},
			"orgB filtering by its own building", fx.devBRacked)
	})

	// The IS NULL branch must not match a rack membership of the other
	// organization.
	t.Run("IncludeNoBuilding", func(t *testing.T) {
		filter := &stores.MinerFilter{IncludeNoBuilding: true}
		assertDevices(t, fx.orgA, filter, "orgA must see only its own no-building rack device", fx.devANoBldg)
		assertDevices(t, fx.orgB, filter, "orgB must see only its own no-building rack device", fx.devBNoBldg)
	})

	// The NOT EXISTS branch must not treat a rack membership of the other
	// organization as evidence that the caller's device is in a rack.
	t.Run("IncludeNoRack", func(t *testing.T) {
		filter := &stores.MinerFilter{IncludeNoRack: true}
		assertDevices(t, fx.orgA, filter, "orgA devices with no orgA rack membership",
			fx.devANoRack, fx.devAForeignRack, fx.devAForeignNoBldg)
		assertDevices(t, fx.orgB, filter, "orgB devices with no orgB rack membership",
			fx.devBNoRack, fx.devBForeignRack, fx.devBForeignNoBldg)
	})

	// Scoped zone_keys carry the building and the zone. The two
	// organizations use the same zone label.
	t.Run("ZoneKeysScoped", func(t *testing.T) {
		scoped := func(buildingID int64) *stores.MinerFilter {
			return &stores.MinerFilter{ZoneKeys: []stores.ZoneKey{{BuildingID: buildingID, Zone: twoOrgSharedZone}}}
		}
		assertDevices(t, fx.orgA, scoped(fx.buildingB), "orgA must see nothing when filtering by orgB's scoped zone")
		assertDevices(t, fx.orgB, scoped(fx.buildingA), "orgB must see nothing when filtering by orgA's scoped zone")

		assertDevices(t, fx.orgA, scoped(fx.buildingA), "orgA filtering by its own scoped zone", fx.devARacked)
		assertDevices(t, fx.orgB, scoped(fx.buildingB), "orgB filtering by its own scoped zone", fx.devBRacked)
	})

	// Wildcard zone_keys skip the cross-organization check of the filter
	// parser, so the dcm.org_id clause is the only boundary for the rack
	// membership.
	t.Run("ZoneKeysWildcard", func(t *testing.T) {
		filter := &stores.MinerFilter{ZoneKeys: []stores.ZoneKey{{BuildingID: 0, Zone: twoOrgSharedZone}}}
		assertDevices(t, fx.orgA, filter, "orgA must see only its own devices under the shared zone label",
			fx.devARacked, fx.devANoBldg)
		assertDevices(t, fx.orgB, filter, "orgB must see only its own devices under the shared zone label",
			fx.devBRacked, fx.devBNoBldg)
	})
}
