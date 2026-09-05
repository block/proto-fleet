package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	inventorymodels "github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestListRepairTicketsUsesLookaheadAndReportsWhetherAnotherPageExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil)
	filter := models.ListFilter{OrgID: 2, Limit: 2}
	store.EXPECT().ListRepairTickets(gomock.Any(), models.ListFilter{OrgID: 2, Limit: 3}).Return(
		[]models.RepairTicketSummary{{RepairTicket: models.RepairTicket{ID: 3}}, {RepairTicket: models.RepairTicket{ID: 2}}, {RepairTicket: models.RepairTicket{ID: 1}}}, nil,
	)
	store.EXPECT().CountRepairTickets(gomock.Any(), filter).Return(int32(3), nil)

	tickets, total, hasNext, err := service.ListRepairTickets(t.Context(), filter)
	require.NoError(t, err)
	assert.Equal(t, int32(3), total)
	assert.True(t, hasNext)
	require.Len(t, tickets, 2)
	assert.Equal(t, int64(2), tickets[1].ID)
}

func TestListCompletedTicketsSuppressesLookaheadOnFinalPage(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil)
	filter := models.CompletedFilter{OrgID: 2, Limit: 2}
	store.EXPECT().ListCompletedTickets(gomock.Any(), models.CompletedFilter{OrgID: 2, Limit: 3}).Return(
		[]models.RepairTicketSummary{{RepairTicket: models.RepairTicket{ID: 1}}}, nil,
	)
	store.EXPECT().CountCompletedTickets(gomock.Any(), filter).Return(int32(1), nil)
	facets := []models.Assignee{{UserID: 9, Username: "former-tech"}}
	store.EXPECT().ListCompletedTicketAssignees(gomock.Any(), int64(2)).Return(facets, nil)

	tickets, total, hasNext, assignees, err := service.ListCompletedTickets(t.Context(), filter)
	require.NoError(t, err)
	assert.Equal(t, int32(1), total)
	assert.False(t, hasNext)
	require.Len(t, tickets, 1)
	assert.Equal(t, facets, assignees)
}

func TestCreateRepairTicketRejectsWhitespaceDiagnosis(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)

	_, err := service.CreateRepairTicket(t.Context(), models.CreateParams{
		OrgID: 2, Category: models.TicketCategoryMiner, Component: "Fan", Diagnosis: stringPointer("   "),
	})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "diagnosis")
}

func TestUpdateRepairTicketRejectsWhitespaceDiagnosis(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{
		OrgID: 2, ID: 3, Diagnosis: stringPointer("   "),
	})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "diagnosis")
}

func TestUpdateRepairTicketRejectsSetAndClearRMAEtaTogether(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)
	eta := time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC)
	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{
		OrgID: 2, ID: 3, RMAEta: &eta, ClearRMAEta: true,
	})
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "set and clear must conflict: %v", err)
}

func TestUpdateRepairTicketTransitionMatrix(t *testing.T) {
	allowed := map[models.TicketStatus]map[models.TicketStatus]bool{
		models.TicketStatusOpen: {
			models.TicketStatusInProgress: true, models.TicketStatusOnHold: true,
			models.TicketStatusSentToVendor: true, models.TicketStatusCompleted: true,
		},
		models.TicketStatusInProgress: {
			models.TicketStatusOpen: true, models.TicketStatusOnHold: true,
			models.TicketStatusSentToVendor: true, models.TicketStatusCompleted: true,
		},
		models.TicketStatusOnHold: {
			models.TicketStatusOpen: true, models.TicketStatusInProgress: true,
			models.TicketStatusSentToVendor: true, models.TicketStatusCompleted: true,
		},
		models.TicketStatusSentToVendor: {
			models.TicketStatusInProgress: true, models.TicketStatusCompleted: true,
		},
		models.TicketStatusCompleted: {},
	}
	statuses := []models.TicketStatus{
		models.TicketStatusOpen, models.TicketStatusInProgress, models.TicketStatusOnHold,
		models.TicketStatusSentToVendor, models.TicketStatusCompleted,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			t.Run(statusName(from)+"-to-"+statusName(to), func(t *testing.T) {
				ctrl := gomock.NewController(t)
				tickets := mocks.NewMockMaintenanceStore(ctrl)
				refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
				inventory := mocks.NewMockInventoryStore(ctrl)
				tx := mocks.NewMockTransactor(ctrl)
				service := NewService(tickets, refs, inventory, tx, nil)
				current := &models.RepairTicket{ID: 10, OrgID: 20, Category: models.TicketCategoryInfrastructure, Status: from, Component: "Transformer", Resolution: models.TicketResolutionDeferred}
				params := models.UpdateParams{ID: 10, OrgID: 20, Status: &to}
				if to == models.TicketStatusSentToVendor {
					vendor := "Vendor"
					params.RMAVendor = &vendor
					params.ExpectedRMASnapshot = &models.RMASnapshot{Status: from}
				}
				if to == models.TicketStatusCompleted {
					resolution := models.TicketResolutionDeferred
					params.Resolution = &resolution
				}
				tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
				tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(20), int64(10)).Return(current, nil)

				isAllowed := from == to || allowed[from][to]
				if isAllowed {
					updated := *current
					updated.Status = to
					if from != models.TicketStatusCompleted {
						if to == models.TicketStatusCompleted {
							tickets.EXPECT().ListTicketParts(gomock.Any(), int64(20), int64(10)).Return(nil, nil)
							tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(20), int64(10)).Return(nil)
						}
						if from != to || params.RMAVendor != nil {
							tickets.EXPECT().UpdateRepairTicket(gomock.Any(), params).Return(&updated, nil)
						}
					}
					got, err := service.UpdateRepairTicket(t.Context(), params)
					require.NoError(t, err)
					assert.Equal(t, to, got.Status)
				} else {
					_, err := service.UpdateRepairTicket(t.Context(), params)
					assert.True(t, fleeterror.IsFailedPreconditionError(err), "transition %d -> %d: %v", from, to, err)
				}
			})
		}
	}
}

func TestValidateCompletionRejectsLocationsForUnsupportedOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		category   models.TicketCategory
		resolution models.TicketResolution
		location   models.RepairLocation
	}{
		{
			name: "infrastructure ticket with repair location", category: models.TicketCategoryInfrastructure,
			resolution: models.TicketResolutionRepaired, location: models.RepairLocationOnRack,
		},
		{
			name: "deferred miner ticket with repair location", category: models.TicketCategoryMiner,
			resolution: models.TicketResolutionDeferred, location: models.RepairLocationRepairBench,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCompletion(tc.category, tc.resolution, tc.location)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "unexpected location must be rejected: %v", err)
		})
	}

	require.NoError(t, validateCompletion(models.TicketCategoryMiner, models.TicketResolutionRepaired, models.RepairLocationOnRack))
	require.NoError(t, validateCompletion(models.TicketCategoryMiner, models.TicketResolutionDeferred, models.RepairLocationUnspecified))
}

func TestCompleteTicketConsumesSelectedPartsOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, inventory, tx, nil)

	status := models.TicketStatusCompleted
	resolution := models.TicketResolutionRepaired
	location := models.RepairLocationOnRack
	selection := []models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 2}}
	expected := []models.PartUsage{}
	params := models.UpdateParams{OrgID: 2, ID: 3, Status: &status, Resolution: &resolution, RepairLocation: &location, PartsSelection: &selection, ExpectedPartsSelection: &expected}
	siteID := int64(11)
	current := &models.RepairTicket{ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryMiner, Status: models.TicketStatusInProgress, Component: "Fan", MinerIdentifier: stringPointer("miner-1")}
	updated := *current
	updated.Status = status

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(current, nil),
		tickets.EXPECT().ListTicketParts(txContextMatcher{}, int64(2), int64(3)).Return(nil, nil),
		inventory.EXPECT().GetForUpdate(txContextMatcher{}, int64(2), int64(7)).Return(&inventorymodels.InventoryPart{ID: 7, OrgID: 2, SiteID: &siteID, Name: "Fan"}, nil),
		inventory.EXPECT().Reserve(txContextMatcher{}, int64(2), int64(7), int32(2)).Return(nil),
		tickets.EXPECT().SetTicketParts(txContextMatcher{}, int64(2), int64(3)).Return(nil),
		tickets.EXPECT().InsertTicketPart(txContextMatcher{}, int64(2), int64(3), int64(7), "Fan", int32(2)).Return(nil),
		inventory.EXPECT().ConsumeReserved(txContextMatcher{}, int64(2), int64(7), int32(2)).Return(nil),
		tickets.EXPECT().MarkTicketPartsConsumed(txContextMatcher{}, int64(2), int64(3)).Return(nil),
		tickets.EXPECT().UpdateRepairTicket(txContextMatcher{}, params).Return(&updated, nil),
	)
	_, err := service.UpdateRepairTicket(t.Context(), params)
	require.NoError(t, err)
}

func TestCompletedTicketRetryDoesNotWriteActivity(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(
		tickets,
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		tx,
		activity.NewService(activityStore),
	)

	status := models.TicketStatusCompleted
	params := models.UpdateParams{OrgID: 2, ID: 3, Status: &status}
	current := &models.RepairTicket{ID: 3, OrgID: 2, TicketNumber: "RPR-3", Status: models.TicketStatusCompleted}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(current, nil)

	ticket, err := service.UpdateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, current, ticket)
}

func TestActiveTicketRetryDoesNotWriteOrEmitActivity(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(
		tickets,
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		tx,
		activity.NewService(activityStore),
	)
	status := models.TicketStatusInProgress
	current := &models.RepairTicket{ID: 3, OrgID: 2, Status: status}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)

	updated, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3, Status: &status})

	require.NoError(t, err)
	assert.Equal(t, current, updated)
}

func TestUpdateRepairTicketRejectsEmptyMutation(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl),
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		mocks.NewMockTransactor(ctrl),
		nil,
	)

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestUpdateRepairTicketRejectsCompletionFieldsWithoutCompletionTransition(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl),
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		mocks.NewMockTransactor(ctrl),
		nil,
	)
	resolution := models.TicketResolutionRepaired

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3, Resolution: &resolution})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestUpdateRepairTicketRejectsRMAFieldsOutsideVendorStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	vendor := "Hidden vendor"
	params := models.UpdateParams{
		OrgID: 2, ID: 3, RMAVendor: &vendor,
		ExpectedRMASnapshot: &models.RMASnapshot{Status: models.TicketStatusOpen},
	}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
		&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen}, nil,
	)

	_, err := service.UpdateRepairTicket(t.Context(), params)

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestUpdateRepairTicketRequiresExpectedRMASnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl),
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		mocks.NewMockTransactor(ctrl),
		nil,
	)
	vendor := "Repair Co"

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3, RMAVendor: &vendor})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "expected_rma_snapshot")
}

func TestUpdateRepairTicketRejectsStaleRMASnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	vendor := "Updated Vendor"
	expectedVendor := "Original Vendor"
	currentVendor := "Concurrent Vendor"
	params := models.UpdateParams{
		OrgID: 2, ID: 3, RMAVendor: &vendor,
		ExpectedRMASnapshot: &models.RMASnapshot{Status: models.TicketStatusSentToVendor, RMAVendor: &expectedVendor},
	}
	current := &models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusSentToVendor, RMAVendor: &currentVendor}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)

	_, err := service.UpdateRepairTicket(t.Context(), params)

	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	assert.ErrorContains(t, err, "RMA details changed")
}

func TestUpdateRepairTicketTreatsBlankTrackingAndNullSnapshotAsEquivalent(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	currentVendor := "Repair Co"
	updatedVendor := "New Repair Co"
	blankTracking := ""
	params := models.UpdateParams{
		OrgID: 2, ID: 3, RMAVendor: &updatedVendor, RMATracking: &blankTracking,
		ExpectedRMASnapshot: &models.RMASnapshot{Status: models.TicketStatusSentToVendor, RMAVendor: &currentVendor},
	}
	current := &models.RepairTicket{
		ID: 3, OrgID: 2, Status: models.TicketStatusSentToVendor,
		RMAVendor: &currentVendor, RMATracking: &blankTracking,
	}
	updated := *current
	updated.RMAVendor = &updatedVendor

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	tickets.EXPECT().UpdateRepairTicket(gomock.Any(), params).Return(&updated, nil)

	got, err := service.UpdateRepairTicket(t.Context(), params)

	require.NoError(t, err)
	assert.Equal(t, updatedVendor, *got.RMAVendor)
}

func TestUpdateRepairTicketDoesNotPersistBlankTrackingOverNull(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	vendor := "Repair Co"
	blankTracking := "  "
	params := models.UpdateParams{
		OrgID: 2, ID: 3, RMAVendor: &vendor, RMATracking: &blankTracking,
		ExpectedRMASnapshot: &models.RMASnapshot{Status: models.TicketStatusSentToVendor, RMAVendor: &vendor},
	}
	current := &models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusSentToVendor, RMAVendor: &vendor}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)

	updated, err := service.UpdateRepairTicket(t.Context(), params)

	require.NoError(t, err)
	assert.Same(t, current, updated)
}

func TestUpdateRepairTicketAcceptsSatisfiedRMARetryWithStaleSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	vendor := "Repair Co"
	status := models.TicketStatusSentToVendor
	params := models.UpdateParams{
		OrgID: 2, ID: 3, Status: &status, RMAVendor: &vendor,
		ExpectedRMASnapshot: &models.RMASnapshot{Status: models.TicketStatusInProgress},
	}
	current := &models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusSentToVendor, RMAVendor: &vendor}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)

	updated, err := service.UpdateRepairTicket(t.Context(), params)

	require.NoError(t, err)
	assert.Same(t, current, updated)
}

func TestUpdateRepairTicketRejectsStalePartSelection(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	selection := []models.PartUsage{}
	expected := []models.PartUsage{}
	params := models.UpdateParams{OrgID: 2, ID: 3, PartsSelection: &selection, ExpectedPartsSelection: &expected}
	current := &models.RepairTicket{ID: 3, OrgID: 2, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return(
		[]models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 1}}, nil,
	)

	_, err := service.UpdateRepairTicket(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestReplacingPartsLocksCurrentAndRequestedInventoryInGlobalOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, nil)
	selection := []models.PartUsage{{InventoryPartID: 5, PartName: "Cable", Quantity: 1}}
	expected := []models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 1}}
	params := models.UpdateParams{OrgID: 2, ID: 3, PartsSelection: &selection, ExpectedPartsSelection: &expected}
	siteID := int64(11)
	current := &models.RepairTicket{ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen, Component: "Power"}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return([]models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 1}}, nil)
	gomock.InOrder(
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(5)).Return(&inventorymodels.InventoryPart{ID: 5, OrgID: 2, SiteID: &siteID, Name: "Cable"}, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(7)).Return(&inventorymodels.InventoryPart{ID: 7, OrgID: 2, SiteID: &siteID, Name: "Fan"}, nil),
		inventory.EXPECT().Reserve(gomock.Any(), int64(2), int64(5), int32(1)).Return(nil),
		inventory.EXPECT().Release(gomock.Any(), int64(2), int64(7), int32(1)).Return(nil),
	)
	tickets.EXPECT().SetTicketParts(gomock.Any(), int64(2), int64(3)).Return(nil)
	tickets.EXPECT().InsertTicketPart(gomock.Any(), int64(2), int64(3), int64(5), "Cable", int32(1)).Return(nil)
	tickets.EXPECT().UpdateRepairTicket(gomock.Any(), gomock.Any()).Return(current, nil)

	_, err := service.UpdateRepairTicket(t.Context(), params)
	require.NoError(t, err)
}

func TestReplacingActivePartsReleasesOldAndReservesNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, nil)
	selection := []models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 1}, {InventoryPartID: 8, PartName: "Cable", Quantity: 2}}
	expected := []models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 3}}
	params := models.UpdateParams{OrgID: 2, ID: 3, PartsSelection: &selection, ExpectedPartsSelection: &expected}
	siteID := int64(11)
	current := &models.RepairTicket{ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen, Component: "Power"}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return([]models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 3}}, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(7)).Return(&inventorymodels.InventoryPart{ID: 7, OrgID: 2, SiteID: &siteID, Name: "Fan"}, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(8)).Return(&inventorymodels.InventoryPart{ID: 8, OrgID: 2, SiteID: &siteID, Name: "Cable"}, nil),
		inventory.EXPECT().Release(gomock.Any(), int64(2), int64(7), int32(2)).Return(nil),
		inventory.EXPECT().Reserve(gomock.Any(), int64(2), int64(8), int32(2)).Return(nil),
		tickets.EXPECT().SetTicketParts(gomock.Any(), int64(2), int64(3)).Return(nil),
		tickets.EXPECT().InsertTicketPart(gomock.Any(), int64(2), int64(3), int64(7), "Fan", int32(1)).Return(nil),
		tickets.EXPECT().InsertTicketPart(gomock.Any(), int64(2), int64(3), int64(8), "Cable", int32(2)).Return(nil),
		tickets.EXPECT().UpdateRepairTicket(gomock.Any(), params).Return(current, nil),
	)
	_, err := service.UpdateRepairTicket(t.Context(), params)
	require.NoError(t, err)
}

func TestDeleteActiveTicketReleasesReservations(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, activity.NewService(activityStore))
	siteID := int64(11)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, SiteID: &siteID}, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return([]models.PartUsage{{InventoryPartID: 7, Quantity: 2}}, nil),
		inventory.EXPECT().Release(gomock.Any(), int64(2), int64(7), int32(2)).Return(nil),
		tickets.EXPECT().SoftDeleteRepairTicket(gomock.Any(), int64(2), int64(3)).Return(int64(1), nil),
	)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		require.NotNil(t, event.SiteID)
		assert.Equal(t, siteID, *event.SiteID)
		return nil
	})
	require.NoError(t, service.DeleteRepairTicket(t.Context(), 2, 3))
}

func TestCompleteTicketStockFailureStopsTicketMutation(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, inventory, tx, nil)
	status := models.TicketStatusCompleted
	resolution := models.TicketResolutionDeferred
	selection := []models.PartUsage{{InventoryPartID: 9, PartName: "PSU", Quantity: 1}}
	expected := []models.PartUsage{}
	params := models.UpdateParams{OrgID: 2, ID: 3, Status: &status, Resolution: &resolution, PartsSelection: &selection, ExpectedPartsSelection: &expected}
	siteID := int64(11)
	current := &models.RepairTicket{ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen, Component: "Power"}
	stockErr := fleeterror.NewFailedPreconditionError("insufficient available stock")

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return(nil, nil)
	inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(9)).Return(&inventorymodels.InventoryPart{ID: 9, OrgID: 2, SiteID: &siteID, Name: "PSU"}, nil)
	inventory.EXPECT().Reserve(gomock.Any(), int64(2), int64(9), int32(1)).Return(stockErr)
	_, err := service.UpdateRepairTicket(t.Context(), params)
	assert.ErrorIs(t, err, stockErr)
}

func TestUpdateRequiresCompletionFieldsAndRMAVendor(t *testing.T) {
	tests := []struct {
		name    string
		current *models.RepairTicket
		params  models.UpdateParams
	}{
		{name: "missing resolution", current: &models.RepairTicket{ID: 3, OrgID: 2, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, params: models.UpdateParams{OrgID: 2, ID: 3, Status: statusPointer(models.TicketStatusCompleted)}},
		{name: "miner repaired without location", current: &models.RepairTicket{ID: 3, OrgID: 2, Category: models.TicketCategoryMiner, Status: models.TicketStatusOpen}, params: models.UpdateParams{OrgID: 2, ID: 3, Status: statusPointer(models.TicketStatusCompleted), Resolution: resolutionPointer(models.TicketResolutionRepaired)}},
		{name: "vendor missing", current: &models.RepairTicket{ID: 3, OrgID: 2, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, params: models.UpdateParams{OrgID: 2, ID: 3, Status: statusPointer(models.TicketStatusSentToVendor)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			tickets := mocks.NewMockMaintenanceStore(ctrl)
			tx := mocks.NewMockTransactor(ctrl)
			service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
			tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
			tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(tt.current, nil)
			_, err := service.UpdateRepairTicket(t.Context(), tt.params)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
		})
	}
}

func TestCreateRepairTicketDerivesMinerContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, buildingID, rackID, assigneeID := int64(11), int64(12), int64(13), int64(14)
	zone, rack, group := "A", "Rack 1", "Hot"
	params := models.CreateParams{OrgID: 4, Category: models.TicketCategoryMiner, Component: " Hashboard ", Diagnosis: stringPointer(" Board failed "), MinerIdentifier: stringPointer(" miner-1 "), AssigneeUserID: &assigneeID}
	expected := params
	expected.Component = "Hashboard"
	expected.Diagnosis = stringPointer("Board failed")
	expected.MinerIdentifier = stringPointer("miner-1")
	expected.SiteID, expected.BuildingID, expected.RackID = &siteID, &buildingID, &rackID
	expected.Zone, expected.RackLabel, expected.GroupLabel = &zone, &rack, &group

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		refs.EXPECT().ResolveAssignee(txContextMatcher{}, int64(4), assigneeID).Return(&models.Assignee{UserID: assigneeID}, nil),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &siteID, BuildingID: &buildingID, Zone: &zone, RackID: &rackID, RackLabel: &rack, GroupLabel: &group}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), siteID).Return(nil),
		refs.EXPECT().LockBuildingForTicket(txContextMatcher{}, int64(4), buildingID).Return(nil),
		tickets.EXPECT().NextTicketNumber(txContextMatcher{}, int64(4)).Return(int64(1), nil),
		tickets.EXPECT().CreateRepairTicket(txContextMatcher{}, expected, "TK-0001").Return(&models.RepairTicket{ID: 1, OrgID: 4, Component: "Hashboard"}, nil),
	)
	ticket, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, "Hashboard", ticket.Component)
}

func TestCreateRepairTicketDerivesMinerContextAndRejectsCrossOrgReference(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, inventory, tx, nil)
	params := models.CreateParams{OrgID: 4, Category: models.TicketCategoryMiner, Component: "Hashboard", MinerIdentifier: stringPointer("miner-1")}
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	refs.EXPECT().ResolveMinerContext(gomock.Any(), int64(4), "miner-1").Return(nil, fleeterror.NewNotFoundError("miner not found"))
	_, err := service.CreateRepairTicket(t.Context(), params)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestCreateCommentTrimsAndBoundsText(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
	siteID := int64(11)
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, SiteID: &siteID}, nil)
	tickets.EXPECT().CreateTicketComment(txContextMatcher{}, int64(2), int64(3), int64(4), "fixed it").Return(&models.TicketComment{ID: 5, Text: "fixed it"}, nil)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		require.NotNil(t, event.SiteID)
		assert.Equal(t, siteID, *event.SiteID)
		return nil
	})
	comment, err := service.CreateComment(t.Context(), 2, 3, 4, "tech", "  fixed it  ")
	require.NoError(t, err)
	assert.Equal(t, "fixed it", comment.Text)

	_, err = service.CreateComment(t.Context(), 2, 3, 4, "tech", string(make([]rune, 4097)))
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestDeleteCommentScopesActivityToTicketSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
	siteID := int64(11)

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().GetTicketCommentSiteForUpdate(txContextMatcher{}, int64(2), int64(4), int64(5)).Return(&siteID, nil),
		tickets.EXPECT().SoftDeleteTicketComment(txContextMatcher{}, int64(2), int64(4), int64(5)).Return(int64(1), nil),
	)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		require.NotNil(t, event.SiteID)
		assert.Equal(t, siteID, *event.SiteID)
		return nil
	})

	require.NoError(t, service.DeleteComment(t.Context(), 2, 4, 5))
}

func TestBulkCloseLocksTicketsAndInventoryBeforeConsumingReservations(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, nil)
	params := models.BulkCloseParams{OrgID: 2, TicketIDs: []int64{4, 3, 3}, Resolution: models.TicketResolutionRepaired, RepairLocation: models.RepairLocationOnRack}
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, Category: models.TicketCategoryMiner, Status: models.TicketStatusOpen}, nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(4)).Return(&models.RepairTicket{ID: 4, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return([]models.PartUsage{{InventoryPartID: 7, Quantity: 1}}, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(4)).Return([]models.PartUsage{{InventoryPartID: 5, Quantity: 2}}, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(5)).Return(&inventorymodels.InventoryPart{ID: 5, OrgID: 2}, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(7)).Return(&inventorymodels.InventoryPart{ID: 7, OrgID: 2}, nil),
		inventory.EXPECT().ConsumeReserved(gomock.Any(), int64(2), int64(7), int32(1)).Return(nil),
		tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), int64(3)).Return(nil),
		inventory.EXPECT().ConsumeReserved(gomock.Any(), int64(2), int64(5), int32(2)).Return(nil),
		tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), int64(4)).Return(nil),
		tickets.EXPECT().BulkCloseTickets(gomock.Any(), int64(2), []int64{3}, int16(models.TicketResolutionRepaired), int16(models.RepairLocationOnRack), nil).Return(int64(1), nil),
		tickets.EXPECT().BulkCloseTickets(gomock.Any(), int64(2), []int64{4}, int16(models.TicketResolutionRepaired), int16(models.RepairLocationUnspecified), nil).Return(int64(1), nil),
	)
	count, err := service.BulkClose(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestBulkCloseRetryWithNoChangesDoesNotLogActivity(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, activity.NewService(activityStore))
	params := models.BulkCloseParams{OrgID: 2, TicketIDs: []int64{3}, Resolution: models.TicketResolutionDeferred}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{
		ID: 3, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusCompleted,
	}, nil)

	count, err := service.BulkClose(t.Context(), params)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestBulkActivityUsesAffectedTicketSiteScope(t *testing.T) {
	assertScope := func(t *testing.T) func(context.Context, *activitymodels.Event) error {
		t.Helper()
		return func(_ context.Context, event *activitymodels.Event) error {
			assert.True(t, event.MultiSite)
			assert.ElementsMatch(t, []int64{11, 12}, event.MemberSiteIDs)
			assert.True(t, event.TouchesUnassigned)
			return nil
		}
	}
	lockedTickets := func() []*models.RepairTicket {
		siteA, siteB := int64(11), int64(12)
		return []*models.RepairTicket{
			{ID: 3, SiteID: &siteA, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen},
			{ID: 4, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen},
			{ID: 5, SiteID: &siteB, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen},
		}
	}

	t.Run("status", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		tickets := mocks.NewMockMaintenanceStore(ctrl)
		tx := mocks.NewMockTransactor(ctrl)
		activityStore := mocks.NewMockActivityStore(ctrl)
		service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
		tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
		for _, ticket := range lockedTickets() {
			tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), ticket.ID).Return(ticket, nil)
		}
		tickets.EXPECT().BulkUpdateTicketStatus(gomock.Any(), int64(2), []int64{3, 4, 5}, int16(models.TicketStatusInProgress)).Return(int64(3), nil)
		activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(assertScope(t))
		_, err := service.BulkUpdateStatus(t.Context(), 2, []int64{5, 3, 4}, models.TicketStatusInProgress)
		require.NoError(t, err)
	})

	t.Run("assignment", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		tickets := mocks.NewMockMaintenanceStore(ctrl)
		refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
		tx := mocks.NewMockTransactor(ctrl)
		activityStore := mocks.NewMockActivityStore(ctrl)
		service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
		assigneeID := int64(9)
		tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
		refs.EXPECT().ResolveAssignee(gomock.Any(), int64(2), assigneeID).Return(&models.Assignee{UserID: assigneeID}, nil)
		for _, ticket := range lockedTickets() {
			tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), ticket.ID).Return(ticket, nil)
		}
		tickets.EXPECT().BulkAssignTickets(gomock.Any(), int64(2), []int64{3, 4, 5}, &assigneeID).Return(int64(3), nil)
		activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(assertScope(t))
		_, err := service.BulkAssign(t.Context(), 2, []int64{5, 3, 4}, &assigneeID)
		require.NoError(t, err)
	})

	t.Run("urgent", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		tickets := mocks.NewMockMaintenanceStore(ctrl)
		tx := mocks.NewMockTransactor(ctrl)
		activityStore := mocks.NewMockActivityStore(ctrl)
		service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
		tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
		for _, ticket := range lockedTickets() {
			tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), ticket.ID).Return(ticket, nil)
		}
		tickets.EXPECT().BulkMarkUrgent(gomock.Any(), int64(2), []int64{3, 4, 5}).Return(int64(3), nil)
		activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(assertScope(t))
		_, err := service.BulkMarkUrgent(t.Context(), 2, []int64{5, 3, 4})
		require.NoError(t, err)
	})

	t.Run("close", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		tickets := mocks.NewMockMaintenanceStore(ctrl)
		tx := mocks.NewMockTransactor(ctrl)
		activityStore := mocks.NewMockActivityStore(ctrl)
		service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
		tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
		for _, ticket := range lockedTickets() {
			tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), ticket.ID).Return(ticket, nil)
			tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), ticket.ID).Return(nil, nil)
			tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), ticket.ID).Return(nil)
		}
		tickets.EXPECT().BulkCloseTickets(gomock.Any(), int64(2), []int64{3, 4, 5}, int16(models.TicketResolutionDeferred), int16(models.RepairLocationUnspecified), nil).Return(int64(3), nil)
		activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(assertScope(t))
		_, err := service.BulkClose(t.Context(), models.BulkCloseParams{OrgID: 2, TicketIDs: []int64{5, 3, 4}, Resolution: models.TicketResolutionDeferred})
		require.NoError(t, err)
	})
}

func TestUpdateRepairTicketRejectsPartFromAnotherSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, nil)
	ticketSiteID, partSiteID := int64(11), int64(12)
	selection := []models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 1}}
	expected := []models.PartUsage{}
	params := models.UpdateParams{OrgID: 2, ID: 3, PartsSelection: &selection, ExpectedPartsSelection: &expected}
	current := &models.RepairTicket{ID: 3, OrgID: 2, SiteID: &ticketSiteID, Category: models.TicketCategoryMiner, Status: models.TicketStatusOpen}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return(nil, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(7)).Return(&inventorymodels.InventoryPart{ID: 7, OrgID: 2, SiteID: &partSiteID}, nil),
	)

	_, err := service.UpdateRepairTicket(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestBulkUpdateStatusSkipsTicketsAlreadyAtTarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(
		tickets,
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		tx,
		activity.NewService(activityStore),
	)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
		&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusInProgress}, nil,
	)

	affected, err := service.BulkUpdateStatus(t.Context(), 2, []int64{3}, models.TicketStatusInProgress)

	require.NoError(t, err)
	assert.Zero(t, affected)
}

func TestBulkUpdateStatusRebuildsChangedIDsOnTransactionRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(
		tickets,
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		tx,
		activity.NewService(activityStore),
	)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
		txCtx := context.WithValue(ctx, txMarker{}, true)
		require.NoError(t, fn(txCtx))
		return fn(txCtx)
	})
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
			&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen}, nil,
		),
		tickets.EXPECT().BulkUpdateTicketStatus(gomock.Any(), int64(2), []int64{3}, int16(models.TicketStatusInProgress)).Return(int64(1), nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
			&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusInProgress}, nil,
		),
	)

	affected, err := service.BulkUpdateStatus(t.Context(), 2, []int64{3}, models.TicketStatusInProgress)

	require.NoError(t, err)
	assert.Zero(t, affected)
}

func TestBulkAssignSkipsTicketsAlreadyAssigned(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(
		tickets,
		refs,
		mocks.NewMockInventoryStore(ctrl),
		tx,
		activity.NewService(activityStore),
	)
	assigneeID := int64(7)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	refs.EXPECT().ResolveAssignee(gomock.Any(), int64(2), assigneeID).Return(&models.Assignee{UserID: assigneeID}, nil)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
		&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen, AssigneeUserID: &assigneeID}, nil,
	)

	affected, err := service.BulkAssign(t.Context(), 2, []int64{3}, &assigneeID)

	require.NoError(t, err)
	assert.Zero(t, affected)
}

func TestBulkMarkUrgentSkipsTicketsAlreadyUrgent(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(
		tickets,
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		tx,
		activity.NewService(activityStore),
	)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
		&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen, Urgent: true}, nil,
	)

	affected, err := service.BulkMarkUrgent(t.Context(), 2, []int64{3})

	require.NoError(t, err)
	assert.Zero(t, affected)
}

func TestBulkUpdateStatusRejectsSentToVendorWithoutVendorData(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl),
		mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl),
		mocks.NewMockTransactor(ctrl),
		nil,
	)

	_, err := service.BulkUpdateStatus(t.Context(), 2, []int64{3}, models.TicketStatusSentToVendor)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestBulkAssignRejectsCompletedTicket(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, Status: models.TicketStatusCompleted}, nil)

	_, err := service.BulkAssign(t.Context(), 2, []int64{3}, nil)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestBulkMarkUrgentRejectsCompletedTicket(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, Status: models.TicketStatusCompleted}, nil)

	_, err := service.BulkMarkUrgent(t.Context(), 2, []int64{3})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestBulkClosePublishesOnlyTheCommittedRetryAttempt(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
	params := models.BulkCloseParams{OrgID: 2, TicketIDs: []int64{3, 4}, Resolution: models.TicketResolutionDeferred}
	open3 := &models.RepairTicket{ID: 3, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}
	open4 := &models.RepairTicket{ID: 4, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}
	completed3 := &models.RepairTicket{ID: 3, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusCompleted}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
		require.NoError(t, fn(ctx))
		return fn(ctx)
	})
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(open3, nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(4)).Return(open4, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return(nil, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(4)).Return(nil, nil),
		tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), int64(3)).Return(nil),
		tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), int64(4)).Return(nil),
		tickets.EXPECT().BulkCloseTickets(gomock.Any(), int64(2), []int64{3, 4}, int16(models.TicketResolutionDeferred), int16(models.RepairLocationUnspecified), nil).Return(int64(2), nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(completed3, nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(4)).Return(open4, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(4)).Return(nil, nil),
		tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), int64(4)).Return(nil),
		tickets.EXPECT().BulkCloseTickets(gomock.Any(), int64(2), []int64{4}, int16(models.TicketResolutionDeferred), int16(models.RepairLocationUnspecified), nil).Return(int64(1), nil),
	)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		assert.Equal(t, []int64{4}, event.Metadata["ticket_ids"])
		assert.Equal(t, int64(1), event.Metadata["affected"])
		return nil
	})

	count, err := service.BulkClose(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestBulkCloseDoesNotRewriteCompletedTickets(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, nil)
	params := models.BulkCloseParams{OrgID: 2, TicketIDs: []int64{3, 4}, Resolution: models.TicketResolutionDeferred}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusCompleted}, nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(4)).Return(&models.RepairTicket{ID: 4, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(4)).Return(nil, nil),
		tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(2), int64(4)).Return(nil),
		tickets.EXPECT().BulkCloseTickets(gomock.Any(), int64(2), []int64{4}, int16(models.TicketResolutionDeferred), int16(models.RepairLocationUnspecified), nil).Return(int64(1), nil),
	)

	count, err := service.BulkClose(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func runResultTx(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	return fn(context.WithValue(ctx, txMarker{}, true))
}

func runTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, txMarker{}, true))
}

type txMarker struct{}

type txContextMatcher struct{}

func (txContextMatcher) Matches(value any) bool {
	ctx, ok := value.(context.Context)
	return ok && ctx.Value(txMarker{}) == true
}

func (txContextMatcher) String() string { return "transaction-bound context" }

func statusName(status models.TicketStatus) string                             { return string(rune('0' + status)) }
func stringPointer(value string) *string                                       { return &value }
func statusPointer(value models.TicketStatus) *models.TicketStatus             { return &value }
func resolutionPointer(value models.TicketResolution) *models.TicketResolution { return &value }
