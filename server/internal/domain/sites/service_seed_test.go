package sites

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// The seed cores (assignBuildingsToSiteInTx / assignRacksToSiteInTx /
// assignDevicesToSiteInTx) are exercised in depth by the AssignBuildingsToSite
// / AssignRacksToSite / AssignDevicesToSite tests. These tests focus on the
// seed orchestration in CreateSite: which sub-assignments fire for a given
// seed shape (skip-level support), the whole-tx slug-collision retry, the
// conflict → rollback signalling, and fail-fast validation.

// Skip-level: a site seeded with ONLY device identifiers (no buildings, no
// racks). The devices become direct site members.
func TestCreateSite_deviceOnlySkipLevel(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	const newSiteID = int64(1)
	identifiers := []string{"d1", "d2"}

	// Pre-tx read (empty network_config → no overlap probe).
	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
	// Create runs inside the tx.
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		Return(&models.Site{ID: newSiteID, Name: "alpha", Slug: "alpha"}, nil)
	// assignDevicesToSiteInTx targeting the freshly-created site.
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSiteID).Return(nil)
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, ptrInt64(newSiteID), identifiers).Return(int64(2), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, ptrInt64(newSiteID)).Return(int64(0), nil)

	res, conflicts, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID, Name: "alpha",
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if res.Site == nil || res.Site.ID != newSiteID {
		t.Fatalf("expected created site id %d, got %+v", newSiteID, res.Site)
	}
	if res.AssignedBuildingCount != 0 || res.AssignedRackCount != 0 {
		t.Fatalf("expected no building/rack seed, got %+v", res)
	}
	if res.ReassignedDeviceCount != 2 {
		t.Fatalf("expected 2 devices assigned, got %d", res.ReassignedDeviceCount)
	}
}

// A site seeded with buildings AND loose racks AND loose devices, all in one
// transaction. Verifies each level's core fires with the new site as target.
func TestCreateSite_mixedBuildingsRacksDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, collStore, nil, nil, tx, nil)

	const newSiteID = int64(1)
	const buildingID = int64(50)
	const rackID = int64(100)
	oldSite := int64(9)
	oldBuilding := int64(70)
	identifiers := []string{"d1"}

	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		Return(&models.Site{ID: newSiteID, Name: "alpha", Slug: "alpha"}, nil)

	// Each core re-locks the (same) target site — three acquisitions total.
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSiteID).Return(nil).Times(3)

	// Buildings core.
	store.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	store.EXPECT().AssignBuildingsToSiteBulk(inTxCtx, testOrgID, []int64{buildingID}, ptrInt64(newSiteID)).Return(int64(1), nil)
	store.EXPECT().ReassignRacksUnderBuildingsBulk(inTxCtx, testOrgID, []int64{buildingID}, ptrInt64(newSiteID)).Return(int64(3), nil)
	store.EXPECT().ReassignDevicesUnderBuildingsBulk(inTxCtx, testOrgID, []int64{buildingID}, ptrInt64(newSiteID)).Return(int64(15), nil)
	buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(inTxCtx, testOrgID, []int64{buildingID}, ptrInt64(newSiteID)).Return(int64(2), nil)

	// Racks core.
	collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &oldSite, BuildingID: &oldBuilding, Zone: "Z"}, nil)
	collStore.EXPECT().UpdateRackPlacementBulkForSite(inTxCtx, testOrgID, []int64{rackID}, ptrInt64(newSiteID)).Return(nil)
	collStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, []int64{rackID}, ptrInt64(newSiteID)).Return(int64(4), nil)
	collStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, gomock.Nil()).Return(int64(0), nil)

	// Devices core.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, ptrInt64(newSiteID), identifiers).Return(int64(1), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, ptrInt64(newSiteID)).Return(int64(0), nil)

	res, conflicts, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID, Name: "alpha",
		BuildingIDs:       []int64{buildingID},
		RackIDs:           []int64{rackID},
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if res.AssignedBuildingCount != 1 || res.AssignedRackCount != 1 || res.ReassignedDeviceCount != 1 {
		t.Fatalf("unexpected seed counts: %+v", res)
	}
}

// A slug collision poisons the surrounding transaction, so the WHOLE create+seed
// must roll back and retry with a fresh slug. Verifies the loop regenerates the
// slug and the second attempt commits.
func TestCreateSite_slugCollisionRetriesWholeTx(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
	gomock.InOrder(
		store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
			DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
				if p.Slug != "north-dc" {
					return nil, errors.New("expected first slug north-dc, got " + p.Slug)
				}
				return nil, models.ErrSiteSlugCollision
			}),
		store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
			DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
				if p.Slug != "north-dc-2" {
					return nil, errors.New("expected retry slug north-dc-2, got " + p.Slug)
				}
				return &models.Site{ID: 1, Name: p.Name, Slug: p.Slug}, nil
			}),
	)

	res, conflicts, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID, Name: "North DC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if res.Site.Slug != "north-dc-2" {
		t.Fatalf("expected retry slug north-dc-2, got %q", res.Site.Slug)
	}
	if tx.calls != 2 {
		t.Fatalf("expected 2 tx attempts, got %d", tx.calls)
	}
}

// A device seed that hits an unresolvable cross-site conflict (force off) must
// return the per-device conflicts and a nil result — the create must not
// "succeed with an empty site".
func TestCreateSite_deviceConflictReturnsConflictsNoResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	const newSiteID = int64(1)
	otherSite := int64(42)
	identifiers := []string{"d1"}

	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		Return(&models.Site{ID: newSiteID, Name: "alpha", Slug: "alpha"}, nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSiteID).Return(nil)
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{"d1": otherSite}, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	// AssignDevicesToSite must NOT run — the batch is rejected.

	res, conflicts, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID, Name: "alpha",
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on conflict, got %+v", res)
	}
	if len(conflicts) != 1 || conflicts[0].Reason != models.ReasonDeviceInRackAtOtherSite {
		t.Fatalf("expected one cross-site conflict, got %v", conflicts)
	}
}

// An invalid network_config is rejected before any read or transaction — the
// create-and-seed path shares CreateSite's network validation.
func TestCreateSite_invalidNetworkConfigBlocksBeforeTx(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)
	// No store expectations — validation must reject before ListSiteSlugs /
	// the transaction.

	_, _, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID, Name: "alpha", NetworkConfig: "not-an-ip",
		DeviceIdentifiers: []string{"d1"},
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if tx.calls != 0 {
		t.Fatalf("expected no transaction to open, got %d", tx.calls)
	}
}
