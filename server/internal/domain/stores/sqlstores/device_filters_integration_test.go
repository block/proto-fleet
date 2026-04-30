package sqlstores_test

import (
	"testing"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZoneFilter_CrossOrgIsolation proves that two orgs sharing the same zone
// label cannot see each other's miners through the zone filter. The filter's
// safety boundary is `device_set_membership.org_id = $orgID` in the EXISTS
// subquery; this test asserts that boundary.
func TestZoneFilter_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	// Two orgs that happen to use the same zone label "shared-zone".
	userA := dbSvc.CreateSuperAdminUser()
	userB := dbSvc.CreateSuperAdminUser2()

	devA := dbSvc.CreateDevice(userA.OrganizationID, "proto")
	devB := dbSvc.CreateDevice(userB.OrganizationID, "proto")

	const sharedZone = "shared-zone"

	// Create a rack collection per org with the same zone label, then add the
	// org's device to its own rack.
	rackA, err := collectionStore.CreateCollection(ctx, userA.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackA.Id, sharedZone, 4, 8, 0, 0, userA.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, userA.OrganizationID, rackA.Id, []string{devA.ID})
	require.NoError(t, err)

	rackB, err := collectionStore.CreateCollection(ctx, userB.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack B", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackB.Id, sharedZone, 4, 8, 0, 0, userB.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, userB.OrganizationID, rackB.Id, []string{devB.ID})
	require.NoError(t, err)

	filter := &stores.MinerFilter{Zones: []string{sharedZone}}

	// Org A should see only its device, never org B's.
	rowsA, _, totalA, err := deviceStore.ListMinerStateSnapshots(ctx, userA.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	require.Len(t, rowsA, 1, "org A should see exactly its own miner under shared-zone")
	assert.Equal(t, devA.ID, rowsA[0].DeviceIdentifier)
	assert.Equal(t, int64(1), totalA, "total count must reflect the filtered org-scoped result")

	// Org B should see only its device, never org A's.
	rowsB, _, totalB, err := deviceStore.ListMinerStateSnapshots(ctx, userB.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	require.Len(t, rowsB, 1, "org B should see exactly its own miner under shared-zone")
	assert.Equal(t, devB.ID, rowsB[0].DeviceIdentifier)
	assert.Equal(t, int64(1), totalB, "total count must reflect the filtered org-scoped result")
}

// TestZoneFilter_ExcludesSoftDeletedRack proves that soft-deleting a rack
// removes its miners from zone-filter results, even though the membership
// rows persist (soft delete only flags device_set.deleted_at).
func TestZoneFilter_ExcludesSoftDeletedRack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	dev := dbSvc.CreateDevice(user.OrganizationID, "proto")

	rack, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Doomed Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rack.Id, "doomed-zone", 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rack.Id, []string{dev.ID})
	require.NoError(t, err)

	filter := &stores.MinerFilter{Zones: []string{"doomed-zone"}}

	// Sanity check: device shows up before deletion.
	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), total)

	// Soft-delete the rack. Membership and rack-extension rows persist.
	_, err = collectionStore.SoftDeleteCollection(ctx, user.OrganizationID, rack.Id)
	require.NoError(t, err)

	// Filter must now return zero — and the total must agree, not just the page.
	rows, _, total, err = deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	assert.Empty(t, rows, "soft-deleted rack must not surface in zone filter results")
	assert.Equal(t, int64(0), total, "total count must agree with the empty page (P1 invariant)")
}
