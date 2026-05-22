package buildings

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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

func TestDeleteBuilding_cascadeUnassignsRacks(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, tx, nil)

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
	svc := NewService(store, siteStore, nil, tx, nil)

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
	svc := NewService(store, siteStore, nil, tx, nil)

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
	svc := NewService(store, siteStore, nil, tx, nil)

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
	svc := NewService(store, siteStore, nil, tx, nil)

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
	svc := NewService(store, siteStore, nil, tx, nil)

	_, err := svc.ListBuildings(context.Background(), models.ListFilter{
		OrgID:          testOrgID,
		SiteID:         ptrInt64(5),
		UnassignedOnly: true,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument error, got nil")
	}
}

func TestCreateBuilding_rejectsInvalidOrderIndex(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, siteStore, nil, tx, nil)

	_, err := svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID:                 testOrgID,
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: models.RackOrderIndex(99),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}
