package buildings

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// The seed cores (assignRacksToBuildingInTx / assignDevicesToBuildingInTx) are
// exercised in depth by the AssignRacksToBuilding / AssignDevicesToBuilding
// tests. These tests focus on the seed orchestration in CreateBuilding: which
// sub-assignments fire for a given seed shape (skip-level support), the
// conflict → rollback signalling, and fail-fast validation. Atomic rollback
// itself is a transaction property covered by integration tests; the fake
// transactor here only proves the method returns the conflict response (and no
// result) so the handler surfaces "nothing created".

// Skip-level: a building seeded with ONLY device identifiers (no racks). The
// devices become direct building members — no rack path runs at all.
func TestCreateBuilding_deviceOnlySkipLevel(t *testing.T) {
	h := newAssignHarness(t)
	const newBuildingID = int64(1)
	identifiers := []string{"d1", "d2"}

	// createBuildingInTx: unassigned building (SiteID nil) skips the site lock.
	h.store.EXPECT().CreateBuilding(inTxCtx, gomock.Any()).
		Return(&models.Building{ID: newBuildingID, Name: "B1"}, nil)
	// No rack path — rack_ids empty.
	// assignDevicesToBuildingInTx targeting the freshly-created building.
	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, newBuildingID).Return(nil)
	h.store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, newBuildingID).Return(nil, nil)
	h.siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	h.store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	h.store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	h.store.EXPECT().AssignDevicesToBuilding(inTxCtx, testOrgID, ptrInt64(newBuildingID), identifiers).Return(int64(2), nil)
	h.store.EXPECT().CascadeDevicesSiteForBuilding(inTxCtx, testOrgID, identifiers, gomock.Nil()).Return(int64(0), nil)

	res, conflicts, err := h.svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID: testOrgID, Name: "B1", DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if res.Building == nil || res.Building.ID != newBuildingID {
		t.Fatalf("expected created building id %d, got %+v", newBuildingID, res.Building)
	}
	if res.AssignedRackCount != 0 {
		t.Fatalf("expected 0 racks assigned, got %d", res.AssignedRackCount)
	}
	if res.ReassignedDeviceCount != 2 {
		t.Fatalf("expected 2 devices assigned, got %d", res.ReassignedDeviceCount)
	}
}

// A building seeded with ONLY rack ids. Racks land as unplaced members (ids
// only; grid positions come later via the manage modal), so pass-2 place never
// runs and no device path fires.
func TestCreateBuilding_rackOnly(t *testing.T) {
	h := newAssignHarness(t)
	const newBuildingID = int64(1)
	const rackID = int64(99)
	siteID := int64(3)

	// createBuildingInTx: building carries a site, so the site row is locked
	// before insert.
	h.siteStore.EXPECT().LockSiteForWrite(inTxCtx, testOrgID, siteID).Return(nil)
	h.store.EXPECT().CreateBuilding(inTxCtx, gomock.Any()).
		Return(&models.Building{ID: newBuildingID, Name: "B1", SiteID: &siteID}, nil)
	// assignRacksToBuildingInTx targeting the new building.
	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, newBuildingID).Return(nil)
	h.store.EXPECT().GetBuilding(inTxCtx, testOrgID, newBuildingID).
		Return(&models.Building{ID: newBuildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil)
	h.store.EXPECT().CountRacksInBuilding(inTxCtx, testOrgID, newBuildingID).Return(int64(0), nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(inTxCtx, rackID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &siteID}, nil)
	// Same site → no site cascade; building changed nil → new → building cascade
	// + bulk placement update + pass-1 vacate fire. No pass-2 place (no cell).
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(inTxCtx, testOrgID, []int64{rackID}, &siteID, ptrInt64(newBuildingID)).Return(int64(1), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(inTxCtx, testOrgID, []int64{rackID}, ptrInt64(newBuildingID)).Return(int64(0), nil)
	h.store.EXPECT().SetRackBuildingPositionBulkClear(inTxCtx, testOrgID, []int64{rackID}).Return(nil)

	res, conflicts, err := h.svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID: testOrgID, Name: "B1", SiteID: &siteID, DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
		RackIDs: []int64{rackID},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero conflicts, got %v", conflicts)
	}
	if res.AssignedRackCount != 1 {
		t.Fatalf("expected 1 rack assigned, got %d", res.AssignedRackCount)
	}
	if res.ReassignedDeviceCount != 0 {
		t.Fatalf("expected 0 devices assigned, got %d", res.ReassignedDeviceCount)
	}
}

// A device seed that hits an unresolvable cross-building conflict (force flag
// off) must return the per-device conflicts and a nil result — the create must
// not "succeed with an empty building".
func TestCreateBuilding_deviceConflictReturnsConflictsNoResult(t *testing.T) {
	h := newAssignHarness(t)
	const newBuildingID = int64(1)
	conflictingBuilding := int64(88)
	identifiers := []string{"d1"}

	h.store.EXPECT().CreateBuilding(inTxCtx, gomock.Any()).
		Return(&models.Building{ID: newBuildingID, Name: "B1"}, nil)
	h.siteStore.EXPECT().LockBuildingForWrite(inTxCtx, testOrgID, newBuildingID).Return(nil)
	h.store.EXPECT().GetBuildingSiteID(inTxCtx, testOrgID, newBuildingID).Return(nil, nil)
	h.siteStore.EXPECT().LockDevicesForReassign(inTxCtx, testOrgID, identifiers).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(inTxCtx, testOrgID, identifiers).Return(identifiers, nil)
	h.store.EXPECT().FindDeviceBuildingConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{"d1": conflictingBuilding}, nil)
	h.store.EXPECT().FindDevicesInBuildingLessPlacedRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(inTxCtx, testOrgID, identifiers).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(inTxCtx, testOrgID, identifiers).Return(nil, nil)
	// AssignDevicesToBuilding must NOT run — the batch is rejected.

	res, conflicts, err := h.svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID: testOrgID, Name: "B1", DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
		DeviceIdentifiers: identifiers,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on conflict, got %+v", res)
	}
	if len(conflicts) != 1 || conflicts[0].Reason != models.ReasonBuildingDeviceInRackAtOtherBuilding {
		t.Fatalf("expected one cross-building conflict, got %v", conflicts)
	}
}

// A seed with no racks and no devices is just a create — no assignment paths
// run.
func TestCreateBuilding_noSeedJustCreates(t *testing.T) {
	h := newAssignHarness(t)
	h.store.EXPECT().CreateBuilding(inTxCtx, gomock.Any()).Return(&models.Building{ID: 5, Name: "B1"}, nil)

	res, conflicts, err := h.svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID: testOrgID, Name: "B1", DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 || res.Building.ID != 5 || res.AssignedRackCount != 0 || res.ReassignedDeviceCount != 0 {
		t.Fatalf("unexpected result %+v conflicts %v", res, conflicts)
	}
}

// Malformed seed (a zero rack id) fails fast before any transaction opens.
func TestCreateBuilding_rejectsInvalidRackIDBeforeTx(t *testing.T) {
	h := newAssignHarness(t)
	// No store EXPECTs — validation must reject before createBuildingInTx runs.

	_, _, err := h.svc.CreateBuilding(context.Background(), models.CreateParams{
		OrgID: testOrgID, Name: "B1", DefaultRackOrderIndex: models.RackOrderIndexBottomLeft,
		RackIDs: []int64{0},
	})
	if !fleeterror.IsInvalidArgumentError(err) {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
	if h.tx.calls != 0 {
		t.Fatalf("expected no transaction to open, got %d", h.tx.calls)
	}
}
