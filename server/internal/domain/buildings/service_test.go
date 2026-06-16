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
// vacate cell in pass 1 (NULL/NULL), then write the actual cell in
// pass 2. No site cascade because target site matches current.
func TestAssignRacksToBuilding_placesRackWithGridCell(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	rackID := int64(99)
	siteID := int64(3)

	gomock.InOrder(
		h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil),
		h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
			Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil),
		// Phase A: lock + read.
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: nil, BuildingID: nil, Zone: ""}, nil),
		// Phase B1: single bulk placement update.
		h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &siteID, &buildingID).Return(int64(1), nil),
		// Phase B2: single bulk cascade — siteChanged (nil -> &siteID).
		h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, []int64{rackID}, &siteID).Return(int64(2), nil),
		// Phase B3: bulk pass-1 vacate.
		h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil),
		// Phase B4: bulk pass-2 place.
		h.store.EXPECT().SetRackBuildingPositionBulkPlace(inTxCtx, testOrgID, []int64{rackID}, []int32{1}, []int32{2}).Return(nil),
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
	// No cascade — site unchanged. Bulk placement update + bulk pass-1
	// vacate fire; pass-2 place is skipped because no positions were
	// requested.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &siteID, &buildingID).Return(int64(1), nil)
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil)

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
// caller resends building_id with no position. The bulk pass-1 vacate
// is what clears the prior (aisle, position) so the unplace doesn't
// silently no-op. Guards against the "unplace within building silently
// no-ops" regression on the post-bulk refactor.
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
	// Bulk placement update — zone preservation is now decided in SQL
	// per-row, so the bulk call only carries (target_site, target_building).
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &siteID, &buildingID).Return(int64(1), nil)
	// Critical: explicit bulk pass-1 vacate fires.
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil)

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
	// Bulk placement update: site target nil (preserve), building target nil.
	// CascadeRackDeviceSitesBulk must NOT fire — site is preserved.
	// Bulk pass-1 vacate is skipped — building_id change inside SQL CASE
	// nulls aisle/position automatically.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, (*int64)(nil), (*int64)(nil)).Return(int64(1), nil)

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
	// Bulk placement update — crossingBuildings zone clear runs in SQL.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &newSite, &targetBuildingID).Return(int64(1), nil)
	// Bulk cascade fires for site-changed racks.
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, []int64{rackID}, &newSite).Return(int64(4), nil)
	// Bulk pass-1 vacate confirms the new row carries no stale placement.
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil)

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

// TestAssignRacksToBuilding_bulkRollsBackOnLaterFailure mirrors the
// sites batch rollback case: first rack succeeds, second errors on the
// lock, the tx aborts, and the closure ran exactly once with the error
// propagating.
func TestAssignRacksToBuilding_bulkRollsBackOnLaterFailure(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	siteID := int64(3)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
		Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil)
	// Phase A walks both rack ids in order; the second lock errors so
	// the closure exits before any bulk write fires.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, int64(100), testOrgID).
		Return(interfaces.RackPlacement{}, nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, int64(101), testOrgID).
		Return(interfaces.RackPlacement{}, fleeterror.NewNotFoundErrorf("rack %d not found", 101))
	// No bulk UpdateRackPlacementBulkForBuilding / CascadeRackDeviceSitesBulk /
	// SetRackBuildingPosition{Bulk}* calls — the closure aborts in Phase A.
	_ = siteID

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

// TestAssignRacksToBuilding_swapsPositionsInSingleBatch covers F5:
// a single batch that swaps two racks' positions inside the same
// building must succeed in one tx. The service pre-clears every
// rack's position (pass 1) before writing any new positions (pass 2),
// so the partial unique index uk_device_set_rack_building_position
// can't see two rows trying to hold the same (building, aisle, pos)
// simultaneously.
func TestAssignRacksToBuilding_swapsPositionsInSingleBatch(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	siteID := int64(3)
	rackA := int64(100)
	rackB := int64(101)

	// Racks are sorted by id, so rackA(100) is processed before rackB(101)
	// during Phase A (lock acquisition). Phase B issues one bulk
	// placement update, then one bulk pass-1 vacate covering both racks,
	// then one bulk pass-2 place that writes the swapped positions in a
	// single statement.
	gomock.InOrder(
		// Building lock + load.
		h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil),
		h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
			Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil),
		// Phase A: locks in sorted order, no writes.
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackA, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(buildingID), Zone: ""}, nil),
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackB, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(buildingID), Zone: ""}, nil),
		// Phase B1: single bulk placement update across both racks.
		h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackA, rackB}, &siteID, &buildingID).Return(int64(2), nil),
		// Phase B2: no cascade — both racks stay in the same site.
		// Phase B3: single bulk pass-1 vacate — critical for the swap.
		h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackA, rackB}).Return(nil),
		// Phase B4: single bulk pass-2 place — both racks in one statement.
		h.store.EXPECT().SetRackBuildingPositionBulkPlace(
			inTxCtx, testOrgID, []int64{rackA, rackB}, []int32{0, 0}, []int32{1, 0},
		).Return(nil),
	)

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks: []models.RackPlacementParam{
			// rackA: (0,0) -> (0,1)
			{RackID: rackA, AisleIndex: ptrInt32(0), PositionInAisle: ptrInt32(1)},
			// rackB: (0,1) -> (0,0)
			{RackID: rackB, AisleIndex: ptrInt32(0), PositionInAisle: ptrInt32(0)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", h.tx.calls)
	}
}

// TestAssignRacksToBuilding_mixedClearAndPlaceInSingleBatch covers F5:
// one rack being unplaced + another rack moving into the freshly
// vacated cell must succeed in one batch. The clear write fires in
// pass 1 strictly before the place write in pass 2.
func TestAssignRacksToBuilding_mixedClearAndPlaceInSingleBatch(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	siteID := int64(3)
	rackClearer := int64(100) // was at (0,0), going to NULL
	rackPlacer := int64(101)  // was unplaced, going to (0,0)

	gomock.InOrder(
		h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil),
		h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
			Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil),
		// Phase A: locks in sorted order.
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackClearer, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(buildingID), Zone: ""}, nil),
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackPlacer, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(buildingID), Zone: ""}, nil),
		// Phase B1: single bulk placement update across both racks.
		h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackClearer, rackPlacer}, &siteID, &buildingID).Return(int64(2), nil),
		// Phase B2: no cascade — both stay in the same site.
		// Phase B3: bulk vacate covers both racks unconditionally
		// (swap-safe invariant).
		h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackClearer, rackPlacer}).Return(nil),
		// Phase B4: bulk pass-2 places only rackPlacer — rackClearer
		// has no requested position and stays vacated.
		h.store.EXPECT().SetRackBuildingPositionBulkPlace(
			inTxCtx, testOrgID, []int64{rackPlacer}, []int32{0}, []int32{0},
		).Return(nil),
	)

	_, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks: []models.RackPlacementParam{
			{RackID: rackClearer}, // clear cell
			{RackID: rackPlacer, AisleIndex: ptrInt32(0), PositionInAisle: ptrInt32(0)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", h.tx.calls)
	}
}

// TestAssignRacksToBuilding_largeBatchIssuesSingleBulkWrites guards the
// F7 bulk refactor: a 100-rack batch must produce exactly one
// UpdateRackPlacementBulkForBuilding + one CascadeRackDeviceSitesBulk
// + one SetRackBuildingPositionBulkClear + one SetRackBuildingPositionBulkPlace
// call regardless of N.
func TestAssignRacksToBuilding_largeBatchIssuesSingleBulkWrites(t *testing.T) {
	h := newAssignHarness(t)
	buildingID := int64(11)
	siteID := int64(3)

	const N = 100
	wantRackIDs := make([]int64, N)
	wantAisles := make([]int32, N)
	wantPositions := make([]int32, N)
	racks := make([]models.RackPlacementParam, N)
	// Build distinct (aisle, position) values that fit in a 10×10 grid.
	for i := 0; i < N; i++ {
		id := int64(1000 + i)
		wantRackIDs[i] = id
		aisle := int32(i / 10)
		pos := int32(i % 10)
		wantAisles[i] = aisle
		wantPositions[i] = pos
		racks[i] = models.RackPlacementParam{
			RackID: id, AisleIndex: ptrInt32(aisle), PositionInAisle: ptrInt32(pos),
		}
	}

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, buildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, buildingID).
		Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 10, RacksPerAisle: 10}, nil)
	// Phase A: N per-rack lock acquisitions in sorted order — these are
	// the only writes that fan out by N.
	for _, id := range wantRackIDs {
		h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, id, testOrgID).
			Return(interfaces.RackPlacement{}, nil)
	}
	// Phase B writes: exactly four bulk calls, regardless of N.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, wantRackIDs, &siteID, &buildingID).Return(int64(len(wantRackIDs)), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, wantRackIDs, &siteID).Return(int64(200), nil)
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, wantRackIDs).Return(nil)
	h.store.EXPECT().SetRackBuildingPositionBulkPlace(inTxCtx, testOrgID, wantRackIDs, wantAisles, wantPositions).Return(nil)

	out, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &buildingID,
		Racks:            racks,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SiteReassignedDeviceCount != 200 {
		t.Fatalf("expected 200 cascaded devices, got %d", out.SiteReassignedDeviceCount)
	}
	if h.tx.calls != 1 {
		t.Fatalf("expected one tx closure run, got %d", h.tx.calls)
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
