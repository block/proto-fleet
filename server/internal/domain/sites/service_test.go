package sites

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
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

// wrappingFakeTransactor mirrors production SQLTransactor: a non-
// FleetError returned by the closure gets wrapped as Internal. Used to
// pin the regression in Fix #1 — a sentinel returned from the closure
// would survive fakeTransactor (returned unchanged) but get rewritten
// in prod, masking the conflict-vs-Internal-error bug.
type wrappingFakeTransactor struct {
	calls int
}

func (f *wrappingFakeTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	err := fn(context.WithValue(ctx, sentinelKey, sentinelValue))
	if err == nil {
		return nil
	}
	var fe fleeterror.FleetError
	if errors.As(err, &fe) {
		return fe
	}
	return fleeterror.NewInternalErrorf("tx: %v", err)
}

func (f *wrappingFakeTransactor) RunInTxWithResult(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	f.calls++
	v, err := fn(context.WithValue(ctx, sentinelKey, sentinelValue))
	if err == nil {
		return v, nil
	}
	var fe fleeterror.FleetError
	if errors.As(err, &fe) {
		return v, fe
	}
	return v, fleeterror.NewInternalErrorf("tx: %v", err)
}

// transactorFactory is the table-driven setup that lets the same case
// run against both the eager fake and the production-shaped wrapping
// fake.
type transactorFactory struct {
	name string
	make func() interfaces.Transactor
}

var transactorFactories = []transactorFactory{
	{name: "fake", make: func() interfaces.Transactor { return &fakeTransactor{} }},
	{name: "wrapping", make: func() interfaces.Transactor { return &wrappingFakeTransactor{} }},
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
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	// activitySvc is nil; the service's logActivity is nil-safe. Production
	// wires a real *activity.Service from main.go.
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	gomock.InOrder(
		store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, int64(11)).Return(nil),
		// Lock every building under the site after the site lock — site→
		// building lock order matches AssignBuildingToSite to prevent
		// deadlock and to keep a concurrent move from slipping a building
		// out of the cascade.
		store.EXPECT().LockBuildingsBySiteForWrite(inTxCtx, testOrgID, int64(11)).Return(nil),
		store.EXPECT().LockInfrastructureDevicesBySiteForWrite(inTxCtx, testOrgID, int64(11)).Return([]int64{70, 71}, nil),
		store.EXPECT().UnassignRacksFromBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(7), nil),
		buildingStore.EXPECT().ClearDeviceBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		store.EXPECT().SoftDeleteBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(2), nil),
		store.EXPECT().UnassignRacksFromSite(inTxCtx, testOrgID, int64(11)).Return(int64(4), nil),
		store.EXPECT().UnassignDevicesFromSite(inTxCtx, testOrgID, int64(11)).Return(int64(3), nil),
		store.EXPECT().DeleteCurtailmentResponseProfilesBySite(inTxCtx, testOrgID, int64(11)).Return(int64(5), nil),
		store.EXPECT().CountResponseProfilesByInfrastructureDevices(inTxCtx, testOrgID, []int64{70, 71}).Return(int64(0), nil),
		store.EXPECT().SoftDeleteInfrastructureDevicesBySite(inTxCtx, testOrgID, int64(11)).Return(int64(6), nil),
		store.EXPECT().SoftDeleteSite(inTxCtx, testOrgID, int64(11)).Return(int64(1), nil),
	)

	out, err := svc.DeleteSite(context.Background(), testOrgID, 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.UnassignedDeviceCount != 3 || out.DeletedBuildingCount != 2 || out.UnassignedRackCount != 4 || out.DeletedResponseProfileCount != 5 || out.DeletedInfrastructureDeviceCount != 6 {
		t.Fatalf("unexpected counts: %+v", out)
	}
	if tx.calls != 1 {
		t.Fatalf("expected exactly one RunInTx, got %d", tx.calls)
	}
}

func TestDeleteSite_notFoundWhenSoftDeleteAffectsZeroRows(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	// LockSiteForWrite succeeds (row exists at start of tx) but the
	// final SoftDeleteSite affects 0 rows because nothing matched the
	// org filter (or the test is asserting the affects-zero defensive
	// branch). All cascade calls happen inside RunInTx.
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, int64(99)).Return(nil)
	store.EXPECT().LockBuildingsBySiteForWrite(inTxCtx, testOrgID, int64(99)).Return(nil)
	store.EXPECT().LockInfrastructureDevicesBySiteForWrite(inTxCtx, testOrgID, int64(99)).Return(nil, nil)
	store.EXPECT().UnassignRacksFromBuildingsBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().SoftDeleteBuildingsBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().UnassignRacksFromSite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().UnassignDevicesFromSite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().DeleteCurtailmentResponseProfilesBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().CountResponseProfilesByInfrastructureDevices(inTxCtx, testOrgID, []int64(nil)).Return(int64(0), nil)
	store.EXPECT().SoftDeleteInfrastructureDevicesBySite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)
	store.EXPECT().SoftDeleteSite(inTxCtx, testOrgID, int64(99)).Return(int64(0), nil)

	_, err := svc.DeleteSite(context.Background(), testOrgID, 99)
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestDeleteSite_rejectsInfrastructureDevicesReferencedBySurvivingProfiles(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	gomock.InOrder(
		store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, int64(11)).Return(nil),
		store.EXPECT().LockBuildingsBySiteForWrite(inTxCtx, testOrgID, int64(11)).Return(nil),
		store.EXPECT().LockInfrastructureDevicesBySiteForWrite(inTxCtx, testOrgID, int64(11)).Return([]int64{70}, nil),
		store.EXPECT().UnassignRacksFromBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		buildingStore.EXPECT().ClearDeviceBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		store.EXPECT().SoftDeleteBuildingsBySite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		store.EXPECT().UnassignRacksFromSite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		store.EXPECT().UnassignDevicesFromSite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		store.EXPECT().DeleteCurtailmentResponseProfilesBySite(inTxCtx, testOrgID, int64(11)).Return(int64(0), nil),
		store.EXPECT().CountResponseProfilesByInfrastructureDevices(inTxCtx, testOrgID, []int64{70}).Return(int64(1), nil),
	)

	_, err := svc.DeleteSite(context.Background(), testOrgID, 11)
	if !fleeterror.IsFailedPreconditionError(err) {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
	if tx.calls != 1 {
		t.Fatalf("expected exactly one RunInTx, got %d", tx.calls)
	}
}

func TestAssignDevicesToSite_rejectsCrossCollectionConflict(t *testing.T) {
	// Table-driven across both transactors: the production-shaped
	// wrapping transactor pins Fix #1 (a sentinel error would get
	// wrapped as Internal in prod, breaking the conflict path).
	for _, tf := range transactorFactories {
		t.Run(tf.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockSiteStore(ctrl)
			tx := tf.make()
			svc := NewService(store, nil, nil, nil, nil, tx, nil)

			identifiers := []string{"d1", "d2"}
			target := int64(20)
			conflictingSite := int64(30)

			// All four store calls happen inside RunInTx; the TOCTOU fix moved
			// the site-alive check into the tx alongside the row lock and
			// switched SiteBelongsToOrg → LockSiteForWrite to defend against
			// concurrent DeleteSite.
			store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
			store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
			store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
			store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
			store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
				"d1": conflictingSite,
			}, nil)
			// No update call — entire batch rejected.

			count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
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
		})
	}
}

func TestAssignDevicesToSite_reportsMissingDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	identifiers := []string{"d1", "d-missing"}
	target := int64(20)

	// Same in-tx set as the rejection path.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)

	_, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
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

func TestAssignDevicesToSite_writesOnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1", "d2"}
	target := int64(20)

	// All five store calls fire inside RunInTx.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(2), nil)
	// Building-mismatch clear runs after the site write to keep
	// device.building_id from pointing at a building in the old site.
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
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

func TestAssignDevicesToSite_unassignedTargetSkipsBelongsCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}

	// Skip LockSiteForWrite when target == nil (Unassigned). The
	// remaining four calls all run inside the tx.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.Nil(), identifiers).Return(int64(1), nil)
	// Unassign also clears any building whose site is non-null (a
	// site-less device can't keep a building that belongs to a site).
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.Nil()).Return(int64(0), nil)

	_, _, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      nil,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssignDevicesToSite_targetMatchesCurrentRackSiteIsNotAConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	target := int64(42)

	// All five calls happen inside the tx.
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": target,
	}, nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(1), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	_, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
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

// TestAssignDevicesToSite_siteLessRackFlagsConflict pins the
// device→site site-consistency guard: a device in a fully-unassigned
// (site-less) rack can't take a direct site while remaining in that
// rack. FindDeviceSiteConflicts misses it (rack site is NULL), so the
// site-less probe flags it as a clearable DEVICE_IN_RACK_AT_OTHER_SITE
// conflict (ConflictingSiteID 0). Without force, the batch rejects.
func TestAssignDevicesToSite_siteLessRackFlagsConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	target := int64(42)

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	// Rack has no site → not a FindDeviceSiteConflicts row; the site-less
	// probe catches it instead.
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	// No write — batch rejects without force.

	_, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      &target,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %v", conflicts)
	}
	if conflicts[0].Reason != models.ReasonDeviceInRackAtOtherSite || conflicts[0].ConflictingSiteID != 0 {
		t.Fatalf("expected DeviceInRackAtOtherSite with ConflictingSiteID 0, got %+v", conflicts[0])
	}
}

// TestAssignDevicesToSite_siteLessRackForceClears pins the force path:
// a device in a site-less rack is unassigned from the rack and then
// takes the target site, keeping device→site consistency.
func TestAssignDevicesToSite_siteLessRackForceClears(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	target := int64(42)

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
	collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, []string{"d1"}, int64(0)).Return(int64(1), nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(1), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:                               testOrgID,
		TargetSiteID:                        &target,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts after force-clear, got %v", conflicts)
	}
	if count != 1 {
		t.Fatalf("expected 1 row updated, got %d", count)
	}
}

// TestAssignDevicesToSite_forceClearCascadesRackMembership pins the
// cross-site reparent path: when the caller passes the force-clear
// flag and devices live in racks at other sites, the service drops
// every rack membership for those devices inside the same tx and
// then applies the site write. No conflicts surface to the caller.
func TestAssignDevicesToSite_forceClearCascadesRackMembership(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d1", "d2"}
	target := int64(20)
	conflictingSite := int64(30)

	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": conflictingSite,
	}, nil)
	// Only d1 had a rack-at-other-site conflict, so only d1's rack
	// memberships get cleared. d2 (no conflict, already at target)
	// keeps its rack row so its rack_slot child isn't cascade-dropped.
	// targetRackID=0 means "exclude nothing" — drop every rack row
	// for the listed devices.
	collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, []string{"d1"}, int64(0)).Return(int64(1), nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(2), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:                               testOrgID,
		TargetSiteID:                        &target,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows updated, got %d", count)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts on cascade-clear success, got %v", conflicts)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", tx.calls)
	}
}

// TestAssignDevicesToSite_forceClearWithoutConflicts is the no-op
// branch: when the flag is true but nothing conflicts, the service
// must not call RemoveDevicesFromAnyRack — the cascade clear is only
// for the rack-at-other-site case.
func TestAssignDevicesToSite_forceClearWithoutConflicts(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d1"}
	target := int64(20)

	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	// No RemoveDevicesFromAnyRack expectation: gomock fails the test
	// if it's called.
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(1), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:                               testOrgID,
		TargetSiteID:                        &target,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row updated, got %d", count)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
}

// TestAssignDevicesToSite_forceClearMissingDeviceStillRejects covers
// the partial-conflict case: cascade-clear handles the rack-site
// mismatch, but a DEVICE_NOT_FOUND still aborts the batch (you can't
// move a device that doesn't exist). The store's AssignDevicesToSite
// must NOT be called when at least one device-not-found conflict
// remains.
func TestAssignDevicesToSite_forceClearMissingDeviceStillRejects(t *testing.T) {
	for _, tf := range transactorFactories {
		t.Run(tf.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockSiteStore(ctrl)
			collStore := mocks.NewMockCollectionStore(ctrl)
			tx := tf.make()
			svc := NewService(store, nil, collStore, nil, nil, tx, nil)

			identifiers := []string{"d1", "d-missing"}
			target := int64(20)
			conflictingSite := int64(30)

			store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
			store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
			// Only d1 exists; d-missing is reported as not found.
			store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return([]string{"d1"}, nil)
			store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
			store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
				"d1": conflictingSite,
			}, nil)
			// Residual DEVICE_NOT_FOUND aborts the tx BEFORE any
			// deletion runs. Otherwise the cascade-clear delete would
			// commit and the tx would still return without the site
			// move, leaving d1 rack-stripped on its old site.
			// No RemoveDevicesFromAnyRack expectation: gomock fails
			// the test if it's called.
			// No AssignDevicesToSite expectation: batch must reject.

			count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
				OrgID:                               testOrgID,
				TargetSiteID:                        &target,
				DeviceIdentifiers:                   identifiers,
				ForceClearConflictingRackMembership: true,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != 0 {
				t.Fatalf("expected zero rows on rejection, got %d", count)
			}
			if len(conflicts) != 1 {
				t.Fatalf("expected one remaining conflict, got %d", len(conflicts))
			}
			if conflicts[0].Reason != models.ReasonDeviceNotFound {
				t.Fatalf("expected ReasonDeviceNotFound, got %v", conflicts[0].Reason)
			}
		})
	}
}

// TestAssignDevicesToSite_forceClearRollsBackOnSiteWriteFailure pins
// the rollback contract: if AssignDevicesToSite fails after the
// cascade clear, the whole tx aborts — the cascade-clear write is
// undone alongside the failed site write. The wrappingFakeTransactor
// also pins that a non-FleetError surfaces as Internal in prod.
func TestAssignDevicesToSite_forceClearRollsBackOnSiteWriteFailure(t *testing.T) {
	for _, tf := range transactorFactories {
		t.Run(tf.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockSiteStore(ctrl)
			collStore := mocks.NewMockCollectionStore(ctrl)
			tx := tf.make()
			svc := NewService(store, nil, collStore, nil, nil, tx, nil)

			identifiers := []string{"d1"}
			target := int64(20)
			conflictingSite := int64(30)
			sentinel := errors.New("site write boom")

			store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
			store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
			store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
			store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
			store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
				"d1": conflictingSite,
			}, nil)
			collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, identifiers, int64(0)).Return(int64(1), nil)
			// Site write fails. The transactor returns the error, which
			// rolls back the cascade clear write that just happened.
			store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(0), sentinel)

			_, _, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
				OrgID:                               testOrgID,
				TargetSiteID:                        &target,
				DeviceIdentifiers:                   identifiers,
				ForceClearConflictingRackMembership: true,
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			// Exactly one tx attempt — RunInTx surfaces the error so the
			// outer caller sees the failure path. No retries are wired
			// into either fake transactor.
			switch ttx := tx.(type) {
			case *fakeTransactor:
				if ttx.calls != 1 {
					t.Fatalf("expected one tx run, got %d", ttx.calls)
				}
			case *wrappingFakeTransactor:
				if ttx.calls != 1 {
					t.Fatalf("expected one tx run, got %d", ttx.calls)
				}
			default:
				t.Fatalf("unexpected transactor type %T", tx)
			}
		})
	}
}

// TestAssignDevicesToSite_forceClearOnlyConflictingDevices pins the
// scoped-clear contract: when only a subset of devices have a
// rack-at-other-site conflict, RemoveDevicesFromAnyRack is called
// only with the conflicting identifiers. Devices already at the
// target site (no conflict) keep their rack rows so their rack_slot
// children aren't cascade-dropped. Regression test for codex PR
// review (issue-420): the original implementation passed the full
// identifier list, over-deleting unrelated rack memberships.
func TestAssignDevicesToSite_forceClearOnlyConflictingDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d-conflict", "d-already-here"}
	target := int64(20)
	conflictingSite := int64(30)

	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	// Only d-conflict is at a different site; d-already-here returns
	// no rack-site row (already at target, would be filtered by the
	// target==site equality check anyway).
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d-conflict": conflictingSite,
	}, nil)
	// Critical: only d-conflict's rack rows get dropped. Passing
	// the full identifier list here would delete d-already-here's
	// rack membership and cascade its rack_slot row.
	collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, []string{"d-conflict"}, int64(0)).Return(int64(1), nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(2), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:                               testOrgID,
		TargetSiteID:                        &target,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows updated, got %d", count)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts after scoped clear, got %v", conflicts)
	}
}

// TestAssignDevicesToSite_forceClearWithUnassignedTarget pins the
// semantic that "Unassigned" (target_site_id == nil) + force_clear
// also strips rack memberships when devices live in racks at any
// site. No site lock is taken (no target). The cascade-clear branch
// fires for the conflicting devices and the site write applies a
// nil target.
func TestAssignDevicesToSite_forceClearWithUnassignedTarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, collStore, nil, nil, tx, nil)

	identifiers := []string{"d1", "d2"}
	conflictingSite := int64(30)

	// No LockSiteForWrite — target is nil (Unassigned).
	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d1": conflictingSite,
		"d2": conflictingSite,
	}, nil)
	// Both devices have rack-at-other-site conflicts; force-clear drops
	// their rack rows.
	collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, identifiers, int64(0)).Return(int64(2), nil)
	// Site write with nil target (unassign).
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.Nil(), identifiers).Return(int64(2), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.Nil()).Return(int64(0), nil)

	count, conflicts, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:                               testOrgID,
		TargetSiteID:                        nil,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows updated, got %d", count)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts on cascade-clear success, got %v", conflicts)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx run, got %d", tx.calls)
	}
}

// TestAssignDevicesToSite_forceClearAuditLogsCascade pins the audit
// trail: when force-clear actually deletes rack memberships, the
// activity event records the cascade side effect alongside the site
// move. Pure site reassignments (no force-clear, or force-clear that
// didn't fire) do not carry the force-clear metadata fields.
func TestAssignDevicesToSite_forceClearAuditLogsCascade(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	tx := &fakeTransactor{}

	captured := []activitymodels.Event{}
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			captured = append(captured, *event)
			return nil
		}).AnyTimes()

	svc := NewService(store, buildingStore, collStore, nil, nil, tx, activity.NewService(mockActivityStore))

	identifiers := []string{"d-clear", "d-already"}
	target := int64(20)
	conflictingSite := int64(30)

	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{
		"d-clear": conflictingSite,
	}, nil)
	collStore.EXPECT().RemoveDevicesFromAnyRack(inTxCtx, testOrgID, []string{"d-clear"}, int64(0)).Return(int64(1), nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(2), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	_, _, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:                               testOrgID,
		TargetSiteID:                        &target,
		DeviceIdentifiers:                   identifiers,
		ForceClearConflictingRackMembership: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected one audit event, got %d", len(captured))
	}
	event := captured[0]
	if event.Type != eventDevicesReassignedToSite {
		t.Fatalf("unexpected event type %q", event.Type)
	}
	got, ok := event.Metadata["force_cleared_rack_membership_count"].(int)
	if !ok {
		t.Fatalf("force_cleared_rack_membership_count missing or wrong type: %#v", event.Metadata["force_cleared_rack_membership_count"])
	}
	if got != 1 {
		t.Fatalf("expected force_cleared_rack_membership_count=1, got %d", got)
	}
	clearedIdents, ok := event.Metadata["force_cleared_device_identifiers"].([]string)
	if !ok {
		t.Fatalf("force_cleared_device_identifiers missing or wrong type: %#v", event.Metadata["force_cleared_device_identifiers"])
	}
	if len(clearedIdents) != 1 || clearedIdents[0] != "d-clear" {
		t.Fatalf("expected [\"d-clear\"], got %v", clearedIdents)
	}
	// Description should mention the rack clear so the audit reader sees
	// the side effect at a glance.
	if !contains(event.Description, "force-cleared") {
		t.Fatalf("expected description to mention force-cleared, got %q", event.Description)
	}
}

// TestAssignDevicesToSite_noForceClearOmitsCascadeMetadata pins the
// negative: a plain site reassignment (no force-clear branch fired)
// must not carry the force-clear metadata fields, since no rack rows
// were deleted.
func TestAssignDevicesToSite_noForceClearOmitsCascadeMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	tx := &fakeTransactor{}

	captured := []activitymodels.Event{}
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			captured = append(captured, *event)
			return nil
		}).AnyTimes()

	svc := NewService(store, buildingStore, nil, nil, nil, tx, activity.NewService(mockActivityStore))

	identifiers := []string{"d1"}
	target := int64(20)

	store.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	store.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	store.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	store.EXPECT().AssignDevicesToSite(inTxCtx, testOrgID, gomock.AssignableToTypeOf(ptrInt64(0)), identifiers).Return(int64(1), nil)
	buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(inTxCtx, testOrgID, identifiers, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	_, _, err := svc.AssignDevicesToSite(context.Background(), models.AssignDevicesToSiteParams{
		OrgID:             testOrgID,
		TargetSiteID:      &target,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected one audit event, got %d", len(captured))
	}
	if _, present := captured[0].Metadata["force_cleared_rack_membership_count"]; present {
		t.Fatalf("force_cleared_rack_membership_count should be absent on plain reassign, got %#v", captured[0].Metadata["force_cleared_rack_membership_count"])
	}
	if _, present := captured[0].Metadata["force_cleared_device_identifiers"]; present {
		t.Fatalf("force_cleared_device_identifiers should be absent on plain reassign")
	}
}

// contains is a tiny helper that avoids dragging in strings just for
// audit-message assertions.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestAssignBuildingsToSite_cascadeOnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	target := int64(20)
	// The TOCTOU fix moved the site-alive check inside the tx and
	// upgraded it to LockSiteForWrite so concurrent DeleteSite can't
	// soft-delete the target between the check and the cascade writes.
	// LockBuildingForWrite serializes against DeleteSite's
	// LockBuildingsBySiteForWrite for the source-site race.
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, int64(50)).Return(nil)
	store.EXPECT().AssignBuildingsToSiteBulk(inTxCtx, testOrgID, []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(1), nil)
	store.EXPECT().ReassignRacksUnderBuildingsBulk(inTxCtx, testOrgID, []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(3), nil)
	store.EXPECT().ReassignDevicesUnderBuildingsBulk(inTxCtx, testOrgID, []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(15), nil)
	// Direct-FK device cascade — covers devices with device.building_id
	// pointing at the moved building but no rack membership.
	buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(inTxCtx, testOrgID, []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(2), nil)

	out, err := svc.AssignBuildingsToSite(context.Background(), models.AssignBuildingsToSiteParams{
		OrgID:        testOrgID,
		BuildingIDs:  []int64{50},
		TargetSiteID: &target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Aggregate counts both cascades: 15 via rack membership + 2 direct-FK.
	if out.ReassignedRackCount != 3 || out.ReassignedDeviceCount != 17 {
		t.Fatalf("unexpected cascade counts: %+v", out)
	}
}

func TestAssignBuildingsToSite_notFoundWhenBuildingMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	target := int64(20)
	// Both calls now live inside the tx after the TOCTOU fix. With the
	// site→building lock order in place, LockBuildingForWrite is the
	// gate that returns NotFound when the building is missing — no
	// AssignBuildingToSite call should follow.
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, int64(50)).
		Return(fleeterror.NewNotFoundErrorf("building %d not found", 50))

	_, err := svc.AssignBuildingsToSite(context.Background(), models.AssignBuildingsToSiteParams{
		OrgID:        testOrgID,
		BuildingIDs:  []int64{50},
		TargetSiteID: &target,
	})
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestAssignBuildingsToSite_bulkRollsBackOnLaterFailure(t *testing.T) {
	// First building succeeds, second fails — tx must roll back, no
	// AssignBuildingToSite call on the second building's cascade phase.
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	target := int64(20)
	// All per-building locks run in Phase A in sorted order. The second
	// building's lock errors so no bulk write fires (cleaner mock log
	// than the pre-refactor per-building loop, where the first building
	// went through update+cascade before the second's lock failed).
	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	store.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, int64(50)).Return(nil)
	store.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, int64(51)).
		Return(fleeterror.NewNotFoundErrorf("building %d not found", 51))

	_, err := svc.AssignBuildingsToSite(context.Background(), models.AssignBuildingsToSiteParams{
		OrgID:        testOrgID,
		BuildingIDs:  []int64{50, 51},
		TargetSiteID: &target,
	})
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound for second building, got %v", err)
	}
}

func TestCreateSite_invalidNetworkConfigBlocksWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)
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
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(0)).Return(nil, nil)
	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
			if p.NetworkConfig != "10.0.0.0/24" {
				return nil, errors.New("expected canonical 10.0.0.0/24, got " + p.NetworkConfig)
			}
			if p.Slug != "alpha" {
				return nil, errors.New("expected slug alpha, got " + p.Slug)
			}
			return &models.Site{ID: 1, Name: p.Name, Slug: p.Slug, NetworkConfig: p.NetworkConfig}, nil
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
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(0)).Return([]models.SiteNetworkConfigEntry{
		{ID: 99, Name: "siteB", NetworkConfig: "10.0.0.0/22"},
	}, nil)
	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
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

func TestCreateSite_generatesNextSlugOnCollision(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return([]string{"north-dc"}, nil)
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
			if p.Slug != "north-dc-2" {
				return nil, errors.New("expected slug north-dc-2, got " + p.Slug)
			}
			return &models.Site{ID: 1, Name: p.Name, Slug: p.Slug}, nil
		})

	out, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID,
		Name:  "North DC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Site.Slug != "north-dc-2" {
		t.Fatalf("expected slug to round-trip, got %q", out.Site.Slug)
	}
}

func TestCreateSite_retriesSlugRace(t *testing.T) {
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

	out, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID,
		Name:  "North DC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Site.Slug != "north-dc-2" {
		t.Fatalf("expected retry slug, got %q", out.Site.Slug)
	}
}

func TestCreateSite_retriesSlugRaceBeyondInitialCollisionWindow(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return(nil, nil)
	attempts := 0
	store.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		AnyTimes().
		DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
			attempts++
			wantSlug := "site"
			if attempts > 1 {
				wantSlug = fmt.Sprintf("site-%d", attempts)
			}
			if p.Slug != wantSlug {
				return nil, errors.New("expected slug " + wantSlug + ", got " + p.Slug)
			}
			if attempts <= 64 {
				return nil, models.ErrSiteSlugCollision
			}
			return &models.Site{ID: 1, Name: p.Name, Slug: p.Slug}, nil
		})

	out, err := svc.CreateSite(context.Background(), models.CreateSiteParams{
		OrgID: testOrgID,
		Name:  "!!!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 65 {
		t.Fatalf("expected 65 create attempts, got %d", attempts)
	}
	if out.Site.Slug != "site-65" {
		t.Fatalf("expected site-65 after collision retries, got %q", out.Site.Slug)
	}
}

func TestUpdateSite_canonicalizesAndPersists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(11)).Return(nil, nil)
	store.EXPECT().GetSite(gomock.Any(), testOrgID, int64(11)).Return(&models.Site{ID: 11, Name: "alpha", Slug: "alpha"}, nil)
	store.EXPECT().UpdateSite(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.UpdateSiteParams) (*models.Site, error) {
			if p.NetworkConfig != "10.0.0.0/24" {
				return nil, errors.New("expected canonical, got " + p.NetworkConfig)
			}
			// Name unchanged → existing slug carried through, not regenerated.
			if p.Slug != "alpha" {
				return nil, errors.New("expected slug alpha, got " + p.Slug)
			}
			return &models.Site{ID: p.ID, Name: p.Name, Slug: p.Slug, NetworkConfig: p.NetworkConfig}, nil
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
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(11)).Return(nil, nil)
	store.EXPECT().GetSite(gomock.Any(), testOrgID, int64(11)).Return(&models.Site{ID: 11, Name: "alpha", Slug: "alpha"}, nil)
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
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), testOrgID, int64(11)).Return([]models.SiteNetworkConfigEntry{
		{ID: 99, Name: "siteB", NetworkConfig: "10.0.0.0/22"},
	}, nil)
	store.EXPECT().GetSite(gomock.Any(), testOrgID, int64(11)).Return(&models.Site{ID: 11, Name: "siteA", Slug: "sitea"}, nil)
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
	svc := NewService(store, nil, nil, nil, nil, tx, nil)
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

func TestUpdateSite_regeneratesSlugOnRename(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().GetSite(gomock.Any(), testOrgID, int64(11)).Return(&models.Site{ID: 11, Name: "North DC", Slug: "north-dc"}, nil)
	// Another live site already owns "south-dc", so the rename must take the
	// next suffix. The renamed site's own slug is excluded from the used set.
	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return([]string{"north-dc", "south-dc"}, nil)
	store.EXPECT().UpdateSite(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.UpdateSiteParams) (*models.Site, error) {
			if p.Slug != "south-dc-2" {
				return nil, errors.New("expected regenerated slug south-dc-2, got " + p.Slug)
			}
			return &models.Site{ID: p.ID, Name: p.Name, Slug: p.Slug}, nil
		})

	out, err := svc.UpdateSite(context.Background(), models.UpdateSiteParams{
		OrgID: testOrgID,
		ID:    11,
		Name:  "South DC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Site.Slug != "south-dc-2" {
		t.Fatalf("expected regenerated slug to round-trip, got %q", out.Site.Slug)
	}
}

func TestUpdateSite_renameReusesOwnSlugBase(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	store.EXPECT().GetSite(gomock.Any(), testOrgID, int64(11)).Return(&models.Site{ID: 11, Name: "North DC", Slug: "north-dc"}, nil)
	// Only this site's own slug is in use, so a rename whose base differs can
	// take the clean base without a suffix (own slug excluded from used set).
	store.EXPECT().ListSiteSlugs(gomock.Any(), testOrgID).Return([]string{"north-dc"}, nil)
	store.EXPECT().UpdateSite(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.UpdateSiteParams) (*models.Site, error) {
			if p.Slug != "west-dc" {
				return nil, errors.New("expected slug west-dc, got " + p.Slug)
			}
			return &models.Site{ID: p.ID, Name: p.Name, Slug: p.Slug}, nil
		})

	out, err := svc.UpdateSite(context.Background(), models.UpdateSiteParams{
		OrgID: testOrgID,
		ID:    11,
		Name:  "West DC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Site.Slug != "west-dc" {
		t.Fatalf("expected slug west-dc, got %q", out.Site.Slug)
	}
}

func TestAssignRacksToSite_emptyRejected(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, mocks.NewMockCollectionStore(ctrl), nil, nil, tx, nil)

	_, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:   testOrgID,
		RackIDs: nil,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty rack_ids, got nil")
	}
}

func TestAssignBuildingsToSite_emptyRejected(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, mocks.NewMockCollectionStore(ctrl), nil, nil, tx, nil)

	_, err := svc.AssignBuildingsToSite(context.Background(), models.AssignBuildingsToSiteParams{
		OrgID:       testOrgID,
		BuildingIDs: nil,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty building_ids, got nil")
	}
}

func TestAssignRacksToSite_nilCollectionStoreRejected(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, nil, nil, nil, tx, nil)

	_, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:   testOrgID,
		RackIDs: []int64{1},
	})
	if err == nil {
		t.Fatal("expected internal error when collection store is unconfigured")
	}
}

// TestAssignRacksToSite_clearsBuildingOnSiteChange covers the cascade
// happy path: a rack moves to a new site, so building_id + zone must
// clear, clearedCount increments, and CascadeRackDeviceSites fires.
func TestAssignRacksToSite_clearsBuildingOnSiteChange(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, collStore, nil, nil, tx, nil)

	rackID := int64(100)
	oldSite := int64(1)
	oldBuilding := int64(50)
	newSite := int64(2)

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSite).Return(nil)
	collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &oldSite, BuildingID: &oldBuilding, Zone: "Z"}, nil)
	// Bulk placement update writes site + clears building/zone in one
	// statement. Bulk cascade follows for the same set.
	collStore.EXPECT().
		UpdateRackPlacementBulkForSite(inTxCtx, testOrgID, []int64{rackID}, &newSite).
		Return(nil)
	collStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, []int64{rackID}, &newSite).Return(int64(4), nil)
	collStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, gomock.Nil()).Return(int64(0), nil)
	_ = oldBuilding

	out, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:        testOrgID,
		RackIDs:      []int64{rackID},
		TargetSiteID: &newSite,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ReassignedDeviceCount != 4 {
		t.Fatalf("expected ReassignedDeviceCount=4, got %d", out.ReassignedDeviceCount)
	}
	if out.ClearedBuildingCount != 1 {
		t.Fatalf("expected ClearedBuildingCount=1, got %d", out.ClearedBuildingCount)
	}
}

// TestAssignRacksToSite_sameSiteIsNoop covers the no-op branch: a rack
// already on the target site stays intact — no cascade, no clear.
func TestAssignRacksToSite_sameSiteIsNoop(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, collStore, nil, nil, tx, nil)

	rackID := int64(100)
	site := int64(7)
	building := int64(50)

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, site).Return(nil)
	collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &site, BuildingID: &building, Zone: "Z"}, nil)
	// Site unchanged → rack is filtered out of the bulk write set; no
	// UpdateRackPlacementBulkForSite or CascadeRackDeviceSitesBulk call
	// fires.
	_ = building

	out, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:        testOrgID,
		RackIDs:      []int64{rackID},
		TargetSiteID: &site,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ReassignedDeviceCount != 0 || out.ClearedBuildingCount != 0 {
		t.Fatalf("expected zero cascade counts, got %+v", out)
	}
}

// TestAssignRacksToSite_noPriorBuildingStaysIntact ensures the
// clearedCount only ticks when there *was* a building to clear.
func TestAssignRacksToSite_noPriorBuildingStaysIntact(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, collStore, nil, nil, tx, nil)

	rackID := int64(100)
	oldSite := int64(1)
	newSite := int64(2)

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSite).Return(nil)
	collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &oldSite, BuildingID: nil, Zone: ""}, nil)
	collStore.EXPECT().
		UpdateRackPlacementBulkForSite(inTxCtx, testOrgID, []int64{rackID}, &newSite).
		Return(nil)
	collStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, []int64{rackID}, &newSite).Return(int64(0), nil)
	collStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, gomock.Nil()).Return(int64(0), nil)

	out, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:        testOrgID,
		RackIDs:      []int64{rackID},
		TargetSiteID: &newSite,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ClearedBuildingCount != 0 {
		t.Fatalf("expected ClearedBuildingCount=0 (no prior building), got %d", out.ClearedBuildingCount)
	}
}

// TestAssignRacksToSite_bulkRollsBackOnLaterFailure mirrors the
// building-batch rollback case: first rack succeeds, second fails on
// the lock, the tx aborts, and the wrapping transactor records exactly
// one closure run with the error propagating up.
func TestAssignRacksToSite_bulkRollsBackOnLaterFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, collStore, nil, nil, tx, nil)

	oldSite := int64(1)
	newSite := int64(2)

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSite).Return(nil)
	// Phase A locks both racks in sorted order. The second lock errors
	// so the closure aborts before any bulk write fires.
	collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, int64(100), testOrgID).
		Return(interfaces.RackPlacement{SiteID: &oldSite}, nil)
	collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, int64(101), testOrgID).
		Return(interfaces.RackPlacement{}, fleeterror.NewNotFoundErrorf("rack %d not found", 101))

	_, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:        testOrgID,
		RackIDs:      []int64{100, 101},
		TargetSiteID: &newSite,
	})
	if !fleeterror.IsNotFoundError(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if tx.calls != 1 {
		t.Fatalf("expected exactly 1 tx closure run, got %d", tx.calls)
	}
}

// TestAssignBuildingsToSite_largeBatchIssuesSingleBulkWrites guards
// the F13 bulk refactor: a 100-building batch must produce exactly one
// AssignBuildingsToSiteBulk + one ReassignRacksUnderBuildingsBulk +
// one ReassignDevicesUnderBuildingsBulk call regardless of N. Per-
// building lock acquisitions stay sequential for deadlock safety.
func TestAssignBuildingsToSite_largeBatchIssuesSingleBulkWrites(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, buildingStore, nil, nil, nil, tx, nil)

	const N = 100
	target := int64(20)
	buildingIDs := make([]int64, N)
	for i := range N {
		buildingIDs[i] = int64(1000 + i)
	}

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, target).Return(nil)
	for _, bid := range buildingIDs {
		store.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, bid).Return(nil)
	}
	store.EXPECT().AssignBuildingsToSiteBulk(inTxCtx, testOrgID, buildingIDs, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(N), nil)
	store.EXPECT().ReassignRacksUnderBuildingsBulk(inTxCtx, testOrgID, buildingIDs, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(300), nil)
	store.EXPECT().ReassignDevicesUnderBuildingsBulk(inTxCtx, testOrgID, buildingIDs, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(2000), nil)
	buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(inTxCtx, testOrgID, buildingIDs, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	out, err := svc.AssignBuildingsToSite(context.Background(), models.AssignBuildingsToSiteParams{
		OrgID:        testOrgID,
		BuildingIDs:  buildingIDs,
		TargetSiteID: &target,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ReassignedRackCount != 300 || out.ReassignedDeviceCount != 2000 {
		t.Fatalf("unexpected cascade counts: %+v", out)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx closure run, got %d", tx.calls)
	}
}

// TestAssignRacksToSite_largeBatchIssuesSingleBulkWrites guards the
// F14 bulk refactor: a 100-rack site-move batch must produce at most
// one UpdateRackPlacementBulkForSite + one CascadeRackDeviceSitesBulk
// call regardless of N, after the per-rack sequential lock phase.
func TestAssignRacksToSite_largeBatchIssuesSingleBulkWrites(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	collStore := mocks.NewMockCollectionStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, collStore, nil, nil, tx, nil)

	const N = 100
	oldSite := int64(9)
	newSite := int64(20)
	rackIDs := make([]int64, N)
	for i := range N {
		rackIDs[i] = int64(1000 + i)
	}

	store.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, newSite).Return(nil)
	for _, rid := range rackIDs {
		collStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rid, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &oldSite}, nil)
	}
	// Exactly one bulk placement update + one bulk cascade.
	collStore.EXPECT().UpdateRackPlacementBulkForSite(inTxCtx, testOrgID, rackIDs, &newSite).Return(nil)
	collStore.EXPECT().CascadeRackDeviceSitesBulk(inTxCtx, testOrgID, rackIDs, &newSite).Return(int64(500), nil)
	collStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, rackIDs, gomock.Nil()).Return(int64(0), nil)

	out, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:        testOrgID,
		RackIDs:      rackIDs,
		TargetSiteID: &newSite,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ReassignedDeviceCount != 500 {
		t.Fatalf("unexpected cascade count: %d", out.ReassignedDeviceCount)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one tx closure run, got %d", tx.calls)
	}
}

// TestAssignBuildingsToSite_rejectsZeroTargetSite guards F12: an
// explicit *TargetSiteID == 0 must return InvalidArgument so callers
// can't confuse "Unassigned" (nil) with a zero-valued site.
func TestAssignBuildingsToSite_rejectsZeroTargetSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, mocks.NewMockCollectionStore(ctrl), nil, nil, tx, nil)

	zero := int64(0)
	_, err := svc.AssignBuildingsToSite(context.Background(), models.AssignBuildingsToSiteParams{
		OrgID:        testOrgID,
		BuildingIDs:  []int64{1},
		TargetSiteID: &zero,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for target_site_id=0, got nil")
	}
	if tx.calls != 0 {
		t.Fatalf("guard must reject before opening tx, got %d", tx.calls)
	}
}

// TestAssignRacksToSite_rejectsZeroTargetSite covers the matching F12
// guard on the rack-batch RPC.
func TestAssignRacksToSite_rejectsZeroTargetSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockSiteStore(ctrl)
	tx := &fakeTransactor{}
	svc := NewService(store, nil, mocks.NewMockCollectionStore(ctrl), nil, nil, tx, nil)

	zero := int64(0)
	_, err := svc.AssignRacksToSite(context.Background(), models.AssignRacksToSiteParams{
		OrgID:        testOrgID,
		RackIDs:      []int64{1},
		TargetSiteID: &zero,
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for target_site_id=0, got nil")
	}
	if tx.calls != 0 {
		t.Fatalf("guard must reject before opening tx, got %d", tx.calls)
	}
}
