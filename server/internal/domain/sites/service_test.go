package sites

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

const testOrgID = int64(7)

// sentinelKey/sentinelValue are stamped into the transactor's child
// context so mock expectations can assert the closure ran inside the
// transactional scope.
type sentinelKeyType struct{}

var sentinelKey = sentinelKeyType{}

const sentinelValue = "in-tx"

// fakeTransactor runs the action eagerly so cascade-unassign happens
// inline in tests without a real DB. It also stamps a sentinel value
// into the child context so EXPECTs can assert calls landed inside the
// closure.
type fakeTransactor struct {
	calls int
}

func (f *fakeTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	return fn(context.WithValue(ctx, sentinelKey, sentinelValue))
}

func (f *fakeTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	f.calls++
	return fn(context.WithValue(ctx, sentinelKey, sentinelValue))
}

// inTxCtx matches a context that carries the sentinel set by
// fakeTransactor — i.e. the call happened inside the transaction.
var inTxCtx = gomock.Cond(func(x any) bool {
	ctx, ok := x.(context.Context)
	if !ok {
		return false
	}
	v, _ := ctx.Value(sentinelKey).(string)
	return v == sentinelValue
})

func ptrInt64(v int64) *int64 { return &v }

func TestDeleteSite_cascadeInOneTransaction(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	// activitySvc is nil; the service's logActivity is nil-safe. Production
	// wires a real *activity.Service from main.go.
	svc := NewService(store, tx, nil)

	gomock.InOrder(
		store.EXPECT().UnassignRacksFromBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(7), nil),
		store.EXPECT().SoftDeleteBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(2), nil),
		store.EXPECT().UnassignRacksFromSite(inTxCtx, testOrgID, int64(11)).Return(int64(4), nil),
		store.EXPECT().UnassignDevicesFromSite(inTxCtx, testOrgID, int64(11)).Return(int64(3), nil),
		store.EXPECT().SoftDeleteSite(inTxCtx, testOrgID, int64(11)).Return(int64(1), nil),
	)

	out, err := svc.DeleteSite(context.Background(), testOrgID, 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.UnassignedDeviceCount != 3 || out.DeletedBuildingCount != 2 || out.UnassignedRackCount != 4 {
		t.Fatalf("unexpected counts: %+v", out)
	}
	if tx.calls != 1 {
		t.Fatalf("expected exactly one RunInTx, got %d", tx.calls)
	}
}

func TestDeleteSite_notFoundWhenSoftDeleteAffectsZeroRows(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	// Cascade steps still run; the SoftDeleteSite at the end returns 0.
	// All 5 calls happen inside RunInTx, so we assert with inTxCtx.
	store.EXPECT().UnassignRacksFromBuildingsBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().SoftDeleteBuildingsBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().UnassignRacksFromSite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().UnassignDevicesFromSite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().SoftDeleteSite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)

	_, err := svc.DeleteSite(context.Background(), testOrgID, 99)
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestReassignDevicesToSite_rejectsCrossCollectionConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	identifiers := []string{"d1", "d2"}
	target := int64(20)
	conflictingSite := int64(30)

	// All four store calls happen inside RunInTx; the TOCTOU fix moved
	// the SiteBelongsToOrg check into the tx alongside the row lock.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().SiteBelongsToOrg(inTxCtx, testOrgID, target).Return(true, nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": conflictingSite,
	}, nil)
	// No update call — entire batch rejected.

	count, conflicts, err := svc.ReassignDevicesToSite(context.Background(), models.ReassignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      &target,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected zero rows on rejection, got %d", count)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected one conflict, got %d", len(conflicts))
	}
	if conflicts[0].DeviceIdentifier != "d1" {
		t.Fatalf("conflict on wrong device: %s", conflicts[0].DeviceIdentifier)
	}
	if conflicts[0].Reason != models.ReasonDeviceInRackAtOtherSite {
		t.Fatalf("wrong reason: %v", conflicts[0].Reason)
	}
	if conflicts[0].ConflictingSiteID != conflictingSite {
		t.Fatalf("wrong conflicting site: %d", conflicts[0].ConflictingSiteID)
	}
	// Tx still ran (lock + checks). Conflict path returns via sentinel error.
	if tx.calls != 1 {
		t.Fatalf("expected exactly one tx run, got %d", tx.calls)
	}
}

func TestReassignDevicesToSite_reportsMissingDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	identifiers := []string{"d1", "d-missing"}
	target := int64(20)

	// Same in-tx set as the rejection path.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().SiteBelongsToOrg(inTxCtx, testOrgID, target).Return(true, nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)

	_, conflicts, err := svc.ReassignDevicesToSite(context.Background(), models.ReassignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      &target,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Reason != models.ReasonDeviceNotFound {
		t.Fatalf("wrong reason: %v", conflicts[0].Reason)
	}
}

func TestReassignDevicesToSite_writesOnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	identifiers := []string{"d1", "d2"}
	target := int64(20)

	// All five store calls fire inside RunInTx.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().SiteBelongsToOrg(inTxCtx, testOrgID, target).Return(true, nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().ReassignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(2), nil)

	count, conflicts, err := svc.ReassignDevicesToSite(context.Background(), models.ReassignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      &target,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows updated, got %d", count)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", tx.calls)
	}
}

func TestReassignDevicesToSite_unassignedTargetSkipsBelongsCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	identifiers := []string{"d1"}

	// Skip SiteBelongsToOrg when target == nil (Unassigned). The
	// remaining four calls all run inside the tx.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().ReassignDevicesToSite(inTxCtx, testOrgID, gomock.Nil(), identifiers).Return(int64(1), nil)

	_, _, err := svc.ReassignDevicesToSite(context.Background(), models.ReassignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      nil,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReassignDevicesToSite_targetMatchesCurrentRackSiteIsNotAConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	identifiers := []string{"d1"}
	target := int64(42)

	// All five calls happen inside the tx.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().SiteBelongsToOrg(inTxCtx, testOrgID, target).Return(true, nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": target,
	}, nil)
	store.EXPECT().ReassignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(1), nil)

	_, conflicts, err := svc.ReassignDevicesToSite(context.Background(), models.ReassignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      &target,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts when device rack matches target, got %v", conflicts)
	}
}

func TestAssignBuildingToSite_cascadeOnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	target := int64(20)
	// SiteBelongsToOrg runs BEFORE RunInTx (precondition check); the
	// remaining three writes live inside the tx and assert with inTxCtx.
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, target).Return(true, nil)
	store.EXPECT().AssignBuildingToSite(inTxCtx, testOrgID, int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(1), nil)
	store.EXPECT().ReassignRacksUnderBuilding(inTxCtx, testOrgID, int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(3), nil)
	store.EXPECT().ReassignDevicesUnderBuilding(inTxCtx, testOrgID, int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(15), nil)

	out, err := svc.AssignBuildingToSite(context.Background(), models.AssignBuildingToSiteParams{
		OrgID:        testOrgID,
		BuildingID:   50,
		TargetSiteID: &target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ReassignedRackCount != 3 || out.ReassignedDeviceCount != 15 {
		t.Fatalf("unexpected cascade counts: %+v", out)
	}
}

func TestAssignBuildingToSite_notFoundWhenBuildingMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	target := int64(20)
	// Pre-tx precondition check uses gomock.Any(); the not-found path
	// happens inside the tx so we use inTxCtx for the write call.
	store.EXPECT().SiteBelongsToOrg(gomock.Any(), testOrgID, target).Return(true, nil)
	store.EXPECT().AssignBuildingToSite(inTxCtx, testOrgID, int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	_, err := svc.AssignBuildingToSite(context.Background(), models.AssignBuildingToSiteParams{
		OrgID:        testOrgID,
		BuildingID:   50,
		TargetSiteID: &target,
	})
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestCreateSite_invalidNetworkConfigBlocksWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)
	// CreateSite must NOT be called when network_config validation fails.

	_, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID:         testOrgID,
		Name:          "alpha",
		NetworkConfig: "not-an-ip",
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestCreateSite_canonicalizesAndPersists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(0)).Return(nil, nil)
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
			if p.NetworkConfig != "10.0.0.0/24" {
				return nil, errors.New("expected canonical 10.0.0.0/24, got " + p.NetworkConfig)
			}
			return &models.Site{ID: 1, Name: p.Name, NetworkConfig: p.NetworkConfig}, nil
		})

	out, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID:         testOrgID,
		Name:          "alpha",
		NetworkConfig: "  10.0.0.0/24  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Site.NetworkConfig != "10.0.0.0/24" {
		t.Fatalf("expected canonical to round-trip back, got %q", out.Site.NetworkConfig)
	}
}

func TestCreateSite_crossSiteOverlapSurfacesAsWarning(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(0)).Return([]models.SiteNetworkConfigEntry{
		{ID: 99, Name: "siteB", NetworkConfig: "10.0.0.0/22"},
	}, nil)
	store.EXPECT().CreateSite(gomock.Any(), gomock.Any()).Return(&models.Site{ID: 1}, nil)

	out, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID:         testOrgID,
		Name:          "siteA",
		NetworkConfig: "10.0.1.0/24",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.NetworkConfigWarnings) == 0 {
		t.Fatal("expected at least one cross-site overlap warning")
	}
}

func TestUpdateSite_canonicalizesAndPersists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(11)).Return(nil, nil)
	store.EXPECT().UpdateSite(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.UpdateSiteParams) (*models.Site, error) {
			if p.NetworkConfig != "10.0.0.0/24" {
				return nil, errors.New("expected canonical, got " + p.NetworkConfig)
			}
			return &models.Site{ID: p.ID, Name: p.Name, NetworkConfig: p.NetworkConfig}, nil
		})

	out, err := svc.UpdateSite(context.Background(), models.UpdateSiteParams{
		OrgID:         testOrgID,
		ID:            11,
		Name:          "alpha",
		NetworkConfig: "  10.0.0.0/24  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Site.NetworkConfig != "10.0.0.0/24" {
		t.Fatalf("expected canonical, got %q", out.Site.NetworkConfig)
	}
}

func TestUpdateSite_excludesSelfFromOverlapWarnings(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(11)).Return(nil, nil)
	store.EXPECT().UpdateSite(gomock.Any(), gomock.Any()).Return(&models.Site{ID: 11}, nil)

	out, err := svc.UpdateSite(context.Background(), models.UpdateSiteParams{
		OrgID:         testOrgID,
		ID:            11,
		Name:          "alpha",
		NetworkConfig: "10.0.0.0/24",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.NetworkConfigWarnings) != 0 {
		t.Fatalf("expected no warnings, got %v", out.NetworkConfigWarnings)
	}
}

func TestUpdateSite_overlapWithDifferentSiteSurfacesWarning(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(11)).Return([]models.SiteNetworkConfigEntry{
		{ID: 99, Name: "siteB", NetworkConfig: "10.0.0.0/22"},
	}, nil)
	store.EXPECT().UpdateSite(gomock.Any(), gomock.Any()).Return(&models.Site{ID: 11}, nil)

	out, err := svc.UpdateSite(context.Background(), models.UpdateSiteParams{
		OrgID:         testOrgID,
		ID:            11,
		Name:          "siteA",
		NetworkConfig: "10.0.1.0/24",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.NetworkConfigWarnings) == 0 {
		t.Fatal("expected overlap warning, got none")
	}
}

func TestUpdateSite_invalidNetworkConfigBlocksWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, tx, nil)
	// UpdateSite must NOT be called when validation fails.

	_, err := svc.UpdateSite(context.Background(), models.UpdateSiteParams{
		OrgID:         testOrgID,
		ID:            11,
		Name:          "alpha",
		NetworkConfig: "not-an-ip",
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}
