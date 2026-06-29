package buildings

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
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

	// All calls happen inside RunInTx; assert via inTxCtx. SoftDeleteBuilding
	// returns the deleted row's site so the audit row is stamped with it.
	deletedSite := int64(7)
	store.EXPECT().SoftDeleteBuilding(inTxCtx, testOrgID, int64(33)).Return(&deletedSite, true, nil)
	store.EXPECT().UnassignRacksFromBuilding(inTxCtx, testOrgID, int64(33)).Return(int64(5), nil)
	store.EXPECT().ClearDeviceBuildingsByBuilding(inTxCtx, testOrgID, int64(33)).Return(int64(0), nil)

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

	// SoftDeleteBuilding reports found=false (no live row) and the cascade
	// short-circuits to NotFound.
	store.EXPECT().SoftDeleteBuilding(inTxCtx, testOrgID, int64(99)).Return(nil, false, nil)

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

func TestListBuildings_rejectsZeroSiteID(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	_, err := svc.ListBuildings(context.Background(), models.ListFilter{
		OrgID:   testOrgID,
		SiteIDs: []int64{5, 0},
	}, fleetlistfilter.Filter{}, nil)
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
		// Phase B2b: building cascade — rack's building changed (nil -> &buildingID).
		h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, &buildingID).Return(int64(2), nil),
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
	// No site cascade — site unchanged. Building cascade DOES fire
	// because the rack went from nil building → &buildingID. Bulk
	// placement update + pass-1 vacate also fire; pass-2 place is
	// skipped because no positions were requested.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &siteID, &buildingID).Return(int64(1), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, &buildingID).Return(int64(0), nil)
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
	// Building cascade DOES fire — rack.building_id went from
	// priorBuildingID → nil, so member devices' building_id has to clear too.
	// Bulk pass-1 vacate is skipped — building_id change inside SQL CASE
	// nulls aisle/position automatically.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, (*int64)(nil), (*int64)(nil)).Return(int64(1), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, gomock.Nil()).Return(int64(0), nil)

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
	// Building cascade fires — rack moved priorBuildingID → targetBuildingID.
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, &targetBuildingID).Return(int64(4), nil)
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
	for i := range N {
		id := int64(1000 + i)
		wantRackIDs[i] = id
		// #nosec G115 -- i is bounded by N=100, fits in int32.
		aisle := int32(i / 10)
		// #nosec G115 -- i is bounded by N=100, fits in int32.
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
	// Phase B writes: bulk calls, regardless of N.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, wantRackIDs, &siteID, &buildingID).Return(int64(len(wantRackIDs)), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, wantRackIDs, &siteID).Return(int64(200), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, wantRackIDs, &buildingID).Return(int64(200), nil)
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

// --- AssignDevicesToBuilding ---

func TestAssignDevicesToBuilding_writesAndCascadesOnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1", "d2"}
	targetBuilding := int64(42)
	targetSite := int64(7)

	// Canonical lock order inside the tx: building -> devices.
	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(&targetSite, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	// Building-less placed-rack probe + cross-site rack probe both fire
	// when target building is non-null.
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToBuilding(inTxCtx, testOrgID, &targetBuilding, identifiers).Return(int64(2), nil)
	// Cascade fires when target_building_id is set and has a site.
	store.EXPECT().CascadeDevicesSiteForBuilding(inTxCtx, testOrgID, identifiers, &targetSite).Return(int64(2), nil)

	result, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  &targetBuilding,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if result.ReassignedCount != 2 {
		t.Fatalf("expected 2 rows reassigned, got %d", result.ReassignedCount)
	}
	if result.SiteReassignedDeviceCount != 2 {
		t.Fatalf("expected 2 devices site-cascaded, got %d", result.SiteReassignedDeviceCount)
	}
}

func TestAssignDevicesToBuilding_rejectsCrossBuildingConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	targetBuilding := int64(42)
	conflictingBuilding := int64(99)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(nil, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": conflictingBuilding,
	}, nil)
	// Building-less placed-rack + site probes both run; d1 is already
	// flagged as a building conflict so empty results here leave the
	// conflict set unchanged.
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	// AssignDevicesToBuilding is NOT called — the batch rejects with conflicts.

	_, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  &targetBuilding,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %v", conflicts)
	}
	if conflicts[0].Reason != models.ReasonBuildingDeviceInRackAtOtherBuilding {
		t.Fatalf("expected reason DeviceInRackAtOtherBuilding, got %v", conflicts[0].Reason)
	}
	if conflicts[0].ConflictingBuildingID != conflictingBuilding {
		t.Fatalf("expected conflicting building %d, got %d", conflictingBuilding, conflicts[0].ConflictingBuildingID)
	}
}

func TestAssignDevicesToBuilding_unassignedTargetSkipsLockAndCascade(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}

	// No LockBuildingForWrite or GetBuildingSiteID when target is nil.
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToBuilding(inTxCtx, testOrgID, gomock.Nil(), identifiers).Return(int64(1), nil)
	// No CascadeDevicesSiteForBuilding on unassign. This svc has no activity
	// sink (nil), so building-unassign also skips the device-set site-scope
	// resolution (#538) — exercised by a dedicated recording-activity test.

	result, _, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  nil,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReassignedCount != 1 {
		t.Fatalf("expected 1 row, got %d", result.ReassignedCount)
	}
}

func TestAssignDevicesToBuilding_forceClearCascadesRackMembership(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d1", "d2"}
	targetBuilding := int64(42)
	conflictingBuilding := int64(99)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(nil, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": conflictingBuilding,
	}, nil)
	// Building-less placed-rack + site probes run for the site-less
	// target building; no extra conflicts beyond the building one
	// already flagged for d1.
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	// d1 had the conflict — its rack memberships get cleared.
	collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, []string{"d1"}, int64(0)).Return(int64(1), nil)
	store.EXPECT().AssignDevicesToBuilding(inTxCtx, testOrgID, &targetBuilding, identifiers).Return(int64(2), nil)
	// Site cascade always fires when target_building_id is set, even when
	// the building is site-less — cascades site_id to NULL so building/
	// site stay in lockstep instead of leaving stale site_id values.
	store.EXPECT().CascadeDevicesSiteForBuilding(inTxCtx, testOrgID, identifiers, gomock.Nil()).Return(int64(0), nil)

	result, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:                               testOrgID,
		TargetBuildingID:                    &targetBuilding,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts after force-clear, got %v", conflicts)
	}
	if result.ReassignedCount != 2 {
		t.Fatalf("expected 2 rows reassigned, got %d", result.ReassignedCount)
	}
}

// TestAssignDevicesToBuilding_forceClearAbortsOnResidualNonClearable
// pins the data-integrity-critical abort: when a force-clear request
// carries a non-clearable conflict (DEVICE_NOT_FOUND) alongside a
// clearable one, the batch must return the residual conflict and write
// NOTHING — no rack-membership delete, no building write. Otherwise the
// tx would commit RemoveDevicesFromAnyRack without the building move,
// stranding rack-stripped devices on their old building.
func TestAssignDevicesToBuilding_forceClearAbortsOnResidualNonClearable(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d1", "d2"}
	targetBuilding := int64(42)
	conflictingBuilding := int64(99)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(nil, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	// d2 is absent from the existing set → DEVICE_NOT_FOUND (residual,
	// non-clearable). d1 is a clearable cross-building conflict.
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": conflictingBuilding,
	}, nil)
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	// CRITICAL: neither the rack-clear nor the building write may run.
	// gomock fails the test if either is called (no EXPECT registered).

	_, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:                               testOrgID,
		TargetBuildingID:                    &targetBuilding,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected only the residual conflict, got %v", conflicts)
	}
	if conflicts[0].DeviceIdentifier != "d2" || conflicts[0].Reason != models.ReasonBuildingDeviceNotFound {
		t.Fatalf("expected residual DEVICE_NOT_FOUND for d2, got %v", conflicts[0])
	}
}

// TestAssignDevicesToBuilding_siteLessBuildingFlagsRackAtRealSite pins
// the cross-site guard for a site-less target building: a device whose
// rack is at a real site can't be moved into an unassigned building
// without first leaving that rack — otherwise the site cascade would
// null device.site_id while the device is still a member of a Site-A
// rack. The probe runs even though the target building has no site, and
// the device surfaces as a clearable conflict.
func TestAssignDevicesToBuilding_siteLessBuildingFlagsRackAtRealSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	targetBuilding := int64(42)
	rackSite := int64(8)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	// Target building is unassigned (site-less).
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(nil, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	// No building conflict and d1's rack has a building (so the
	// building-less probe is empty); the site probe flags d1's rack at
	// a real site against the site-less target.
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": rackSite,
	}, nil)
	// No force-clear → batch rejects; no write.

	_, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  &targetBuilding,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %v", conflicts)
	}
	if conflicts[0].Reason != models.ReasonBuildingDeviceInRackAtOtherSite {
		t.Fatalf("expected reason DeviceInRackAtOtherSite, got %v", conflicts[0].Reason)
	}
}

// TestAssignDevicesToBuilding_flagsBuildingLessPlacedRack pins the gap
// the building + site probes miss on their own: a device in a rack that
// has a site but no building, assigned to a building in that SAME site.
// The building probe skips it (rack building NULL), the site probe skips
// it (rack site == target site), but assigning it would leave the device
// directly in a building while its rack has none — so the building-less
// placed-rack probe must flag it as a clearable conflict.
func TestAssignDevicesToBuilding_flagsBuildingLessPlacedRack(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	targetBuilding := int64(42)
	site := int64(8)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	// Target building is in site 8.
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(&site, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	// No building conflict (rack has no building) and no site conflict
	// (d1's rack is in the same site 8) — only the building-less placed
	// rack probe catches it.
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": site, // same site as target → not a site conflict
	}, nil)
	// No force-clear → batch rejects; no write.

	_, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  &targetBuilding,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %v", conflicts)
	}
	if conflicts[0].Reason != models.ReasonBuildingDeviceInRackAtOtherBuilding {
		t.Fatalf("expected reason DeviceInRackAtOtherBuilding, got %v", conflicts[0].Reason)
	}
}

// TestAssignDevicesToBuilding_flagsFullyUnassignedRack pins the gap the
// other probes miss entirely: a device in a rack with NEITHER site nor
// building. FindDeviceBuildingConflicts (rack building set),
// FindDevicesInBuildingLessPlacedRacks (rack site set), and
// FindDeviceSiteConflicts (rack site set) all require some placement, so
// only the site-less-rack probe catches it. Assigning to a building
// cascades site, so it must be flagged (clearable) to avoid leaving the
// device with a site while in a site-less rack.
func TestAssignDevicesToBuilding_flagsFullyUnassignedRack(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	targetBuilding := int64(42)
	site := int64(8)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(&site, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	// Rack is fully unassigned — every placement-based probe is empty;
	// only the site-less-rack probe flags it.
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	// No force-clear → batch rejects; no write.

	_, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  &targetBuilding,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %v", conflicts)
	}
	if conflicts[0].Reason != models.ReasonBuildingDeviceInRackAtOtherSite {
		t.Fatalf("expected reason DeviceInRackAtOtherSite, got %v", conflicts[0].Reason)
	}
}

// TestAssignDevicesToBuilding_siteLessRackAlreadyInTargetBuildingNoConflict
// pins the fix for the false positive where a miner already in a rack at
// the target SITE-LESS building was re-flagged. The rack has site NULL
// (its building is site-less) so FindDevicesInSiteLessRacks returns it,
// but its building already equals the target — the building probe treats
// it as a no-op, and the site-less guard must skip it too (rack has a
// building → not a fully-unassigned rack).
func TestAssignDevicesToBuilding_siteLessRackAlreadyInTargetBuildingNoConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	targetBuilding := int64(42)

	siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuilding).Return(nil)
	// Target building is site-less.
	store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, targetBuilding).Return(nil, nil)
	siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	// d1's rack is already at the target building → not a conflict.
	store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": targetBuilding,
	}, nil)
	store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	// Rack is site-less (building is site-less) so this returns d1 — the
	// guard must NOT flag it because its rack already has the target
	// building.
	siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	// No conflict → the move proceeds.
	store.EXPECT().AssignDevicesToBuilding(inTxCtx, testOrgID, &targetBuilding, identifiers).Return(int64(1), nil)
	store.EXPECT().CascadeDevicesSiteForBuilding(inTxCtx, testOrgID, identifiers, gomock.Nil()).Return(int64(0), nil)

	result, conflicts, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID:             testOrgID,
		TargetBuildingID:  &targetBuilding,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict for a miner already in the target site-less building, got %v", conflicts)
	}
	if result.ReassignedCount != 1 {
		t.Fatalf("expected 1 reassigned, got %d", result.ReassignedCount)
	}
}

// TestAssignRacksToBuilding_sameSiteCascadesBuilding covers the gap the
// site cascade alone leaves: moving a rack to a different building
// inside the same site doesn't touch device.site_id, but
// device.building_id still has to follow. Pins that the building
// cascade fires independently of the site cascade.
func TestAssignRacksToBuilding_sameSiteCascadesBuilding(t *testing.T) {
	h := newAssignHarness(t)
	targetBuildingID := int64(22)
	priorBuildingID := int64(11)
	rackID := int64(99)
	siteID := int64(3)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, targetBuildingID).
		Return(&models.Building{ID: targetBuildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &siteID, BuildingID: ptrInt64(priorBuildingID), Zone: "Z1"}, nil)
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &siteID, &targetBuildingID).Return(int64(1), nil)
	// Site cascade MUST NOT fire — both prior and new site are siteID.
	// Building cascade MUST fire — rack moved priorBuildingID → targetBuildingID.
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, &targetBuildingID).Return(int64(2), nil)
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil)

	out, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &targetBuildingID,
		Racks:            []models.RackPlacementParam{{RackID: rackID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SiteReassignedDeviceCount != 0 {
		t.Fatalf("expected zero site cascade (same site), got %d", out.SiteReassignedDeviceCount)
	}
}

// TestAssignRacksToBuilding_unassignedRackToSiteLessBuildingCascadesSiteNull
// pins the lockstep fix for a fully-unassigned rack (no site, no building)
// placed into a site-less building. The rack's own site stays nil->nil, so
// the site gate alone wouldn't fire — but the building cascade stamps the
// site-less building and members may carry a direct device.site_id that
// must be cleared to NULL to stay consistent. The site cascade must run
// with a nil target for the building-changed rack.
func TestAssignRacksToBuilding_unassignedRackToSiteLessBuildingCascadesSiteNull(t *testing.T) {
	h := newAssignHarness(t)
	targetBuildingID := int64(22)
	rackID := int64(99)

	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, targetBuildingID).Return(nil)
	// Target building is site-less.
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, targetBuildingID).
		Return(&models.Building{ID: targetBuildingID, SiteID: nil}, nil)
	// Rack is fully unassigned: no site, no building.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: nil, BuildingID: nil}, nil)
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, (*int64)(nil), &targetBuildingID).Return(int64(1), nil)
	// Site cascade MUST fire with a nil target even though rack.site_id
	// didn't change (nil->nil) — members' direct device.site_id gets
	// cleared to match the site-less building.
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, []int64{rackID}, gomock.Nil()).Return(int64(3), nil)
	// Building cascade stamps device.building_id = target.
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, &targetBuildingID).Return(int64(3), nil)
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil)

	out, err := h.svc.AssignRacksToBuilding(context.Background(), models.AssignRacksToBuildingParams{
		OrgID:            testOrgID,
		TargetBuildingID: &targetBuildingID,
		Racks:            []models.RackPlacementParam{{RackID: rackID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SiteReassignedDeviceCount != 3 {
		t.Fatalf("expected 3 site-cascaded devices, got %d", out.SiteReassignedDeviceCount)
	}
}

func TestAssignDevicesToBuilding_rejectsEmptyIdentifiers(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, nil, nil, tx, nil)

	_, _, err := svc.AssignDevicesToBuilding(context.Background(), models.AssignDevicesToBuildingParams{
		OrgID: testOrgID,
	})
	if !fleeterror.IsInvalidArgumentError(err) {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}
