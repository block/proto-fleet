package buildings

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

const testOrgID = int64(7)

// sentinelKey/sentinelValue mark the closure context so EXPECTs can
// assert calls landed inside RunInTx.
type sentinelKeyType struct{}

var sentinelKey = sentinelKeyType{}

const sentinelValue = "in-tx"

type fakeTransactor struct{ calls int }

func (f *fakeTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	return fn(context.WithValue(ctx, sentinelKey, sentinelValue))
}

func (f *fakeTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	f.calls++
	return fn(context.WithValue(ctx, sentinelKey, sentinelValue))
}

// inTxCtx matches a context carrying the sentinel set by fakeTransactor —
// i.e. the call landed inside the transaction.
var inTxCtx = gomock.Cond(func(x any) bool {
	ctx, ok := x.(context.Context)
	if !ok {
		return false
	}
	v, _ := ctx.Value(sentinelKey).(string)
	return v == sentinelValue
})

func ptrInt64(v int64) *int64 { return &v }
func ptrInt32(v int32) *int32 { return &v }

func TestDeleteBuilding_cascadeUnassignsRacks(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	// Both calls happen inside RunInTx; assert via inTxCtx.
	store.EXPECT().SoftDeleteBuilding(inTxCtx, testOrgID, int64(33)).Return(int64(1), nil)
	store.EXPECT().UnassignRacksFromBuilding(inTxCtx, testOrgID, int64(33)).Return(int64(5), nil)

	out, err := svc.DeleteBuilding(context.Background(), testOrgID, 33)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.UnassignedRackCount != 5 {
		t.Fatalf("expected 5 racks unassigned, got %d", out.UnassignedRackCount)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", tx.calls)
	}
}

func TestDeleteBuilding_notFoundWhenSoftDeleteAffectsZeroRows(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	// SoftDeleteBuilding runs inside the tx and returns 0; cascade short-circuits.
	store.EXPECT().SoftDeleteBuilding(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)

	_, err := svc.DeleteBuilding(context.Background(), testOrgID, 99)
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestCreateBuilding_rejectsUnknownSiteID(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	// The race-fix wraps CreateBuilding in a tx and replaces the
	// SiteBelongsToOrg pre-check with LockSiteForWrite. When the site is
	// missing/already soft-deleted, LockSiteForWrite returns NotFound and
	// the insert never runs.
	siteStore.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, int64(123)).
		Return(fleeterror.NewNotFoundErrorf("site %d not found", 123))

	_, err := svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		SiteID:                ptrInt64(123),
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestCreateBuilding_unassignedSkipsSiteCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	// LockSiteForWrite must not be invoked when SiteID is nil. The insert
	// still runs inside the tx (inTxCtx asserts that).
	store.EXPECT().CreateBuilding(inTxCtx, gomock.Any()).Return(&models.Building{ID: 1, Name: "Aisle-1"}, nil)

	_, err := svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		SiteID:                nil,
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", tx.calls)
	}
}

func TestCreateBuilding_withSiteLocksAndPersists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	// Site→insert ordering inside the tx, both with inTxCtx.
	gomock.InOrder(
		siteStore.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, int64(42)).Return(nil),
		store.EXPECT().CreateBuilding(inTxCtx, gomock.AssignableToTypeOf(models.CreateParams{})).
			Return(&models.Building{ID: 9, Name: "Aisle-9", SiteID: ptrInt64(42)}, nil),
	)

	b, err := svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		SiteID:                ptrInt64(42),
		Name:                  "Aisle-9",
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil || b.ID != 9 {
		t.Fatalf("unexpected building: %+v", b)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", tx.calls)
	}
}

func TestListBuildings_rejectsExclusiveFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	_, err := svc.ListBuildings(context.Background(), models.ListFilter{
		OrgID:          testOrgID,
		SiteID:         ptrInt64(5),
		UnassignedOnly: true,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument error, got nil")
	}
}

// Helper: assemble the full mock set for AssignRacksToBuilding tests.
type assignHarness struct {
	store           *mocks.MockBuildingStore
	siteStore       *mocks.MockSiteStore
	collectionStore *mocks.MockCollectionStore
	tx              *fakeTransactor
	svc             *Service
}

func newAssignHarness(t *testing.T) *assignHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, collectionStore, nil, nil, tx, nil)
	return &assignHarness{
		store:           store,
		siteStore:       siteStore,
		collectionStore: collectionStore,
		tx:              tx,
		svc:             svc,
	}
}

// Assign with a grid cell: lock building, lock rack, write placement,
// write grid cell, no site cascade because target site matches current.
func TestAssignRacksToBuilding_placesRackWithGridCell(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	rackID := int64(99)
	siteID := int64(3)

	gomock.InOrder(
		h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil),
		h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
			Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil),
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: nil, BuildingID: nil, Zone: ""}, nil),
		h.collectionStore.EXPECT().UpdateRackPlacement(inTxCtx, rackID, testOrgID, &siteID, &buildingID, "").Return(nil),
		// siteChanged is true (nil -> &siteID); cascade fires.
		h.collectionStore.EXPECT().CascadeRackDeviceSites(inTxCtx, rackID, testOrgID, &siteID).Return(int64(2), nil),
		h.store.EXPECT().SetRackBuildingPosition(inTxCtx, testOrgID, rackID, ptrInt32(1), ptrInt32(2)).Return(nil),
	)

	out, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks: []models.RackPlacementParam{{
			RackID:          rackID,
			AisleIndex:      ptrInt32(1),
			PositionInAisle: ptrInt32(2),
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SiteReassignedDeviceCount != 2 {
		t.Fatalf("expected 2 cascaded devices, got %d", out.SiteReassignedDeviceCount)
	}
	if h.tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", h.tx.calls)
	}
}

// Assign without a grid cell: writes placement + clears position via
// SetRackBuildingPosition(nil, nil). The explicit clear is what makes
// same-building unplace work — without it, UpdateRackPlacement's CASE
// preserves the old position whenever building_id doesn't change.
func TestAssignRacksToBuilding_membersWithoutPositionClearsCell(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	rackID := int64(99)
	siteID := int64(3)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
		Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &siteID}, nil)
	h.collectionStore.EXPECT().UpdateRackPlacement(inTxCtx, rackID, testOrgID, &siteID, &buildingID, "").Return(nil)
	h.store.EXPECT().SetRackBuildingPosition(inTxCtx, testOrgID, rackID, (*int32)(nil), (*int32)(nil)).Return(nil)

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks:            []models.RackPlacementParam{{RackID: rackID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Same-building unplace: rack already in this building at a known cell,
// caller resends building_id with no position. SetRackBuildingPosition
// must fire with nil/nil so the prior (aisle, position) is cleared from
// the rack row. Guards against the "unplace within building silently
// no-ops" regression.
func TestAssignRacksToBuilding_sameBuildingUnplaceClearsPosition(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	rackID := int64(99)
	siteID := int64(3)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
		Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(buildingID), Zone: "Z1"}, nil)
	// Same building → zone preserved → finalZone is "Z1".
	h.collectionStore.EXPECT().UpdateRackPlacement(inTxCtx, rackID, testOrgID, &siteID, &buildingID, "Z1").Return(nil)
	// No site change → no cascade.
	// Critical: explicit position clear fires.
	h.store.EXPECT().SetRackBuildingPosition(inTxCtx, testOrgID, rackID, (*int32)(nil), (*int32)(nil)).Return(nil)

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks:            []models.RackPlacementParam{{RackID: rackID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Building-only unassign must preserve the rack's site_id — the cascade
// from the device level was the bug this test guards against. siteChanged
// is false, so CascadeRackDeviceSites must never be called.
func TestAssignRacksToBuilding_unassignPreservesSiteAndSkipsCascade(t *testing.T) {
	h := newAssignHarness(t)
	const rackID = int64(99)
	const priorBuildingID = int64(11)
	siteID := int64(3)

	// No LockBuildingForWrite / GetBuilding expected — params.BuildingID is nil.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(priorBuildingID), Zone: "Z1"}, nil)
	// site stays &siteID; zone clears (leavingBuilding).
	h.collectionStore.EXPECT().UpdateRackPlacement(inTxCtx, rackID, testOrgID, &siteID, (*int64)(nil), "").Return(nil)
	// CascadeRackDeviceSites must NOT fire.

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: nil,
		Racks:            []models.RackPlacementParam{{RackID: rackID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Cross-building move into a different site: zone clears, site cascade
// runs with the new site.
func TestAssignRacksToBuilding_crossBuildingClearsZoneAndCascadesSite(t *testing.T) {
	h := newAssignHarness(t)
	targetBuildingID := int64(22)
	priorBuildingID := int64(11)
	rackID := int64(99)
	priorSite := int64(3)
	newSite := int64(7)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, targetBuildingID).
		Return(&models.Building{ID: targetBuildingID, SiteID: &newSite, Aisles: 4, RacksPerAisle: 6}, nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &priorSite, BuildingID: ptrInt64(priorBuildingID), Zone: "Z1"}, nil)
	// crossingBuildings ⇒ zone clears.
	h.collectionStore.EXPECT().UpdateRackPlacement(inTxCtx, rackID, testOrgID, &newSite, &targetBuildingID, "").Return(nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSites(inTxCtx, rackID, testOrgID, &newSite).Return(int64(4), nil)
	// Cross-building move with no chosen cell — explicit nil/nil
	// position write confirms the new row carries no stale placement.
	h.store.EXPECT().SetRackBuildingPosition(inTxCtx, testOrgID, rackID, (*int32)(nil), (*int32)(nil)).Return(nil)

	out, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: ptrInt64(targetBuildingID),
		Racks:            []models.RackPlacementParam{{RackID: rackID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SiteReassignedDeviceCount != 4 {
		t.Fatalf("expected 4 cascaded devices, got %d", out.SiteReassignedDeviceCount)
	}
}

// Grid cell out of bounds: validated after GetBuilding, before any write.
func TestAssignRacksToBuilding_rejectsOutOfBoundsAisle(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	rackID := int64(99)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
		Return(&models.Building{ID: buildingID, Aisles: 2, RacksPerAisle: 6}, nil)
	// Reach LockRackPlacementForWrite via the closure ordering, but no
	// write or cascade fires because validation rejects first.

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: ptrInt64(buildingID),
		Racks: []models.RackPlacementParam{{
			RackID:          rackID,
			AisleIndex:      ptrInt32(2), // out of bounds (Aisles=2 means valid 0,1)
			PositionInAisle: ptrInt32(0),
		}},
	})
	if err == nil {
		t.Fatal("expected InvalidArgument, got nil")
	}
}

// Position pairing: aisle_index set, position_in_aisle absent.
func TestAssignRacksToBuilding_rejectsHalfSetPosition(t *testing.T) {
	h := newAssignHarness(t)
	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: ptrInt64(11),
		Racks: []models.RackPlacementParam{{
			RackID:     1,
			AisleIndex: ptrInt32(0),
		}},
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for half-set position pair, got nil")
	}
}

// Position-requires-building guard: grid cell set, building_id nil.
func TestAssignRacksToBuilding_rejectsPositionWithoutBuilding(t *testing.T) {
	h := newAssignHarness(t)
	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: nil,
		Racks: []models.RackPlacementParam{{
			RackID:          1,
			AisleIndex:      ptrInt32(0),
			PositionInAisle: ptrInt32(0),
		}},
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for grid cell without building_id, got nil")
	}
}

// TestAssignRacksToBuilding_emptyRejected guards the len(Racks) == 0
// pre-check so callers learn up front instead of getting a 0-row
// response.
func TestAssignRacksToBuilding_emptyRejected(t *testing.T) {
	h := newAssignHarness(t)
	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: ptrInt64(11),
		Racks:            nil,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty racks, got nil")
	}
	if h.tx.calls != 0 {
		t.Fatalf("guard must reject before opening tx, got %d", h.tx.calls)
	}
}

// TestAssignRacksToBuilding_rejectsDuplicateRackIDs covers F19: bulk
// requests with the same rack id repeated must fail up-front so the
// per-entry grid-cell write doesn't silently clobber an earlier entry.
func TestAssignRacksToBuilding_rejectsDuplicateRackIDs(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks: []models.RackPlacementParam{
			{RackID: 1, AisleIndex: ptrInt32(0), PositionInAisle: ptrInt32(0)},
			{RackID: 1, AisleIndex: ptrInt32(0), PositionInAisle: ptrInt32(1)},
		},
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for duplicate rack_ids, got nil")
	}
	if h.tx.calls != 0 {
		t.Fatalf("guard must reject before opening tx, got %d", h.tx.calls)
	}
}

// TestAssignRacksToBuilding_bulkRollsBackOnLaterFailure pins the
// rollback contract for the per-rack loop: the first rack's
// placement + cascade writes happen, then the second rack's lock
// errors and the whole tx aborts. The closure ran exactly once.
func TestAssignRacksToBuilding_bulkRollsBackOnLaterFailure(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	siteID := int64(3)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
		Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil)
	// First rack: lock + placement update + cascade + position write.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, int64(100), testOrgID).
		Return(interfaces.RackPlacement{}, nil)
	h.collectionStore.EXPECT().UpdateRackPlacement(inTxCtx, int64(100), testOrgID, &siteID, &buildingID, "").Return(nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSites(inTxCtx, int64(100), testOrgID, &siteID).Return(int64(0), nil)
	h.store.EXPECT().SetRackBuildingPosition(inTxCtx, testOrgID, int64(100), (*int32)(nil), (*int32)(nil)).Return(nil)
	// Second rack: lock errors → tx aborts.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, int64(101), testOrgID).
		Return(interfaces.RackPlacement{}, fleeterror.NewNotFoundErrorf("rack %d not found", 101))

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks: []models.RackPlacementParam{
			{RackID: 100},
			{RackID: 101},
		},
	})
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if h.tx.calls != 1 {
		t.Fatalf("expected exactly 1 tx closure run, got %d", h.tx.calls)
	}
}

// ListBuildingRacks just delegates to the store after an org-scoped
// building existence check.
func TestListBuildingRacks_returnsStoreResult(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	h.store.EXPECT().GetBuilding(gomock.Any(), testOrgID, buildingID).
		Return(&models.Building{ID: buildingID}, nil)
	h.store.EXPECT().ListBuildingRacks(gomock.Any(), testOrgID, buildingID, gomock.Any(), gomock.Any()).
		Return([]models.BuildingRack{{RackID: 1, RackLabel: "A"}}, "next-page", nil)

	racks, nextPageToken, err := h.svc.ListBuildingRacks(context.Background(), testOrgID, buildingID, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(racks) != 1 || racks[0].RackLabel != "A" {
		t.Fatalf("unexpected racks: %+v", racks)
	}
	if nextPageToken != "next-page" {
		t.Fatalf("expected next-page token to propagate, got %q", nextPageToken)
	}
}

// Shrinking aisles or racks_per_aisle below an existing rack's
// placement must abort the update — without this guard the FE silently
// hides out-of-bounds rows and stale (aisle, position) rows persist.
func TestUpdateBuilding_rejectsShrinkThatOrphansPlacement(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, int64(11)).Return(nil)
	store.EXPECT().GetBuilding(inTxCtx, testOrgID, int64(11)).
		Return(&models.Building{ID: 11, Aisles: 5, RacksPerAisle: 6}, nil)
	// Shrink check uses the unbounded bounds-only query.
	store.EXPECT().ListRacksOutsideBuildingBounds(inTxCtx, testOrgID, int64(11), int32(3), int32(6)).
		Return([]models.BuildingRack{
			{RackID: 99, RackLabel: "Edge", AisleIndex: ptrInt32(4), PositionInAisle: ptrInt32(0)},
		}, nil)
	// UpdateBuilding must NOT be called when the bounds check rejects.

	_, err := svc.UpdateBuilding(context.Background(), models.UpdateParams{
		OrgID:                 testOrgID,
		ID:                    11,
		Name:                  "shrunk",
		Aisles:                3,
		RacksPerAisle:         6,
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if !fleeterror.IsInvalidArgumentError(err) {
		t.Fatalf("expected InvalidArgument for orphaning shrink, got %v", err)
	}
}

// Service-edge bounds cap mirrors the proto buf.validate cap. Defense
// in depth for non-proto callers (sdk / agent-native paths) that
// bypass the wire validator.
func TestCreateBuilding_rejectsLayoutAbove100(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	_, err := svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		Name:                  "Huge",
		Aisles:                101,
		RacksPerAisle:         50,
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if !fleeterror.IsInvalidArgumentError(err) {
		t.Fatalf("expected InvalidArgument for aisles>100, got %v", err)
	}

	_, err = svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		Name:                  "Huge",
		Aisles:                50,
		RacksPerAisle:         101,
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if !fleeterror.IsInvalidArgumentError(err) {
		t.Fatalf("expected InvalidArgument for racks_per_aisle>100, got %v", err)
	}
}

// Layout growth (or no-shrink layout edit) must skip the
// ListBuildingRacks bounds-scan entirely; that path used to fire
// no scan at all, so the test pins the new behavior to the shrink
// branch only.
func TestUpdateBuilding_growthSkipsBoundsScan(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, int64(11)).Return(nil)
	store.EXPECT().GetBuilding(inTxCtx, testOrgID, int64(11)).
		Return(&models.Building{ID: 11, Aisles: 2, RacksPerAisle: 4}, nil)
	// No ListBuildingRacks expected — growth path.
	store.EXPECT().UpdateBuilding(inTxCtx, gomock.AssignableToTypeOf(models.UpdateParams{})).
		Return(&models.Building{ID: 11, Aisles: 5, RacksPerAisle: 6}, nil)

	_, err := svc.UpdateBuilding(context.Background(), models.UpdateParams{
		OrgID:                 testOrgID,
		ID:                    11,
		Name:                  "grown",
		Aisles:                5,
		RacksPerAisle:         6,
		DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateBuilding_rejectsInvalidOrderIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	_, err := svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: models.RackOrderIndex(99),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}
