package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	inventorymodels "github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var testVersion = time.Unix(1700000000, 123456000)

func TestListRepairTicketsUsesLookaheadAndReportsWhetherAnotherPageExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil)
	filter := models.ListFilter{OrgID: 2, Limit: 2}
	store.EXPECT().ListRepairTickets(gomock.Any(), models.ListFilter{OrgID: 2, Limit: 3}).Return(
		[]models.RepairTicketSummary{{RepairTicket: models.RepairTicket{UpdatedAt: testVersion, ID: 3}}, {RepairTicket: models.RepairTicket{UpdatedAt: testVersion, ID: 2}}, {RepairTicket: models.RepairTicket{UpdatedAt: testVersion, ID: 1}}}, nil,
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
		[]models.RepairTicketSummary{{RepairTicket: models.RepairTicket{UpdatedAt: testVersion, ID: 1}}}, nil,
	)
	store.EXPECT().CountCompletedTickets(gomock.Any(), filter).Return(int32(1), nil)
	facets := []models.Assignee{{UserID: 9, Username: "former-tech"}}
	store.EXPECT().ListCompletedTicketAssignees(gomock.Any(), filter).Return(facets, nil)

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
		OrgID: 2, IdempotencyKey: "create-ticket-whitespace", Category: models.TicketCategoryMiner, Component: "Fan", Diagnosis: stringPointer("   "),
	})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "diagnosis")
}

func TestCreateRepairTicketRequiresIdempotencyKey(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)

	_, err := service.CreateRepairTicket(t.Context(), models.CreateParams{
		OrgID: 2, Category: models.TicketCategoryMiner, Component: "Fan", MinerIdentifier: stringPointer("miner-1"),
	})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "idempotency_key")
}

func TestUpdateRepairTicketRejectsWhitespaceDiagnosis(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{ExpectedUpdatedAt: testVersion,
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
	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{ExpectedUpdatedAt: testVersion,
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
				current := &models.RepairTicket{UpdatedAt: testVersion, ID: 10, OrgID: 20, Category: models.TicketCategoryInfrastructure, Status: from, Component: "Transformer", Resolution: models.TicketResolutionDeferred}
				params := models.UpdateParams{ExpectedUpdatedAt: testVersion, ID: 10, OrgID: 20, Status: &to}
				if to == models.TicketStatusSentToVendor {
					vendor := "Vendor"
					params.RMAVendor = &vendor
				}
				if to == models.TicketStatusCompleted {
					resolution := models.TicketResolutionDeferred
					params.Resolution = &resolution
				}
				tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
				tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(20), int64(10)).Return(current, nil)

				isAllowed := from != models.TicketStatusCompleted && (from == to || allowed[from][to])
				if isAllowed {
					updated := *current
					updated.Status = to
					if from != models.TicketStatusCompleted {
						if to == models.TicketStatusCompleted {
							tickets.EXPECT().ListTicketParts(gomock.Any(), int64(20), int64(10)).Return(nil, nil)
							tickets.EXPECT().MarkTicketPartsConsumed(gomock.Any(), int64(20), int64(10)).Return(nil)
						}
						{
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

	params := models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, Status: &status, Resolution: &resolution, RepairLocation: &location, PartsSelection: &selection}
	siteID := int64(11)
	current := &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryMiner, Status: models.TicketStatusInProgress, Component: "Fan", MinerIdentifier: stringPointer("miner-1")}
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

func TestCompleteTicketRequiresExpectedVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)
	status := models.TicketStatusCompleted

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3, Status: &status})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "expected_updated_at")
}

func TestCompletedTicketRejectsUnsatisfiedCompletionRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	status := models.TicketStatusCompleted

	resolution := models.TicketResolutionReplaced
	params := models.UpdateParams{ExpectedUpdatedAt: testVersion,
		OrgID: 2, ID: 3, Status: &status, Resolution: &resolution,
	}
	current := &models.RepairTicket{UpdatedAt: testVersion,
		ID: 3, OrgID: 2, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusCompleted,
		Resolution: models.TicketResolutionDeferred,
	}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)

	_, err := service.UpdateRepairTicket(t.Context(), params)

	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	assert.ErrorContains(t, err, "terminal")
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

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3})

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

	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, Resolution: &resolution})

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestUpdateRepairTicketRejectsRMAFieldsOutsideVendorStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	vendor := "Hidden vendor"
	params := models.UpdateParams{ExpectedUpdatedAt: testVersion,
		OrgID: 2, ID: 3, RMAVendor: &vendor,
	}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(
		&models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, Status: models.TicketStatusOpen}, nil,
	)

	_, err := service.UpdateRepairTicket(t.Context(), params)

	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestUpdateRepairTicketTreatsBlankTrackingAndNullSnapshotAsEquivalent(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	currentVendor := "Repair Co"
	updatedVendor := "New Repair Co"
	blankTracking := ""
	params := models.UpdateParams{ExpectedUpdatedAt: testVersion,
		OrgID: 2, ID: 3, RMAVendor: &updatedVendor, RMATracking: &blankTracking,
	}
	current := &models.RepairTicket{UpdatedAt: testVersion,
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

func TestReplacingPartsLocksCurrentAndRequestedInventoryInGlobalOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), inventory, tx, nil)
	selection := []models.PartUsage{{InventoryPartID: 5, PartName: "Cable", Quantity: 1}}

	params := models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, PartsSelection: &selection}
	siteID := int64(11)
	current := &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen, Component: "Power"}

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

	params := models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, PartsSelection: &selection}
	siteID := int64(11)
	current := &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen, Component: "Power"}

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

func TestUpdateRepairTicketRejectsLockedTicketOutsideAuthorizedSiteScope(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	deniedSiteID := int64(9)
	urgent := true
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{OrgWide: true, SiteIDs: []int64{deniedSiteID}})

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	tickets.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(
		&models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, SiteID: &deniedSiteID, Status: models.TicketStatusOpen}, nil,
	)

	_, err := service.UpdateRepairTicket(ctx, models.UpdateParams{ExpectedUpdatedAt: testVersion, ID: 3, OrgID: 2, Urgent: &urgent})
	assert.True(t, fleeterror.IsForbiddenError(err))
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

	params := models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, Status: &status, Resolution: &resolution, PartsSelection: &selection}
	siteID := int64(11)
	current := &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, SiteID: &siteID, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen, Component: "Power"}
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
		{name: "missing resolution", current: &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, params: models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, Status: statusPointer(models.TicketStatusCompleted)}},
		{name: "miner repaired without location", current: &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, Category: models.TicketCategoryMiner, Status: models.TicketStatusOpen}, params: models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, Status: statusPointer(models.TicketStatusCompleted), Resolution: resolutionPointer(models.TicketResolutionRepaired)}},
		{name: "vendor missing", current: &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, params: models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, Status: statusPointer(models.TicketStatusSentToVendor)}},
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

func TestCreateInfrastructureTicketRevalidatesBuildingSitePairAfterLocking(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, buildingID := int64(11), int64(12)
	params := models.CreateParams{
		OrgID:          4,
		IdempotencyKey: "create-infrastructure-ticket",
		Category:       models.TicketCategoryInfrastructure,
		Component:      "Network",
		SiteID:         &siteID,
		BuildingID:     &buildingID,
	}
	movedErr := fleeterror.NewFailedPreconditionError("building no longer belongs to site")

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-infrastructure-ticket").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-infrastructure-ticket").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveLocationContext(txContextMatcher{}, int64(4), &siteID, &buildingID).
			Return(&models.AssetContext{SiteID: &siteID, BuildingID: &buildingID}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), siteID).Return(nil),
		refs.EXPECT().LockBuildingForTicket(txContextMatcher{}, int64(4), buildingID).Return(nil),
		refs.EXPECT().ResolveLocationContext(txContextMatcher{}, int64(4), &siteID, &buildingID).
			Return(nil, movedErr),
	)

	_, err := service.CreateRepairTicket(t.Context(), params)
	assert.ErrorIs(t, err, movedErr)
}

func TestCreateRepairTicketReturnsOriginalTicketForMatchingIdempotentReplay(t *testing.T) {
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
	minerID := "miner-1"
	minerIDWithWhitespace := " miner-1 "
	params := models.CreateParams{
		OrgID:           4,
		IdempotencyKey:  "create-ticket-1",
		Category:        models.TicketCategoryMiner,
		Component:       "Hashboard",
		MinerIdentifier: &minerIDWithWhitespace,
	}
	existing := &models.RepairTicket{UpdatedAt: testVersion,
		ID:              9,
		OrgID:           4,
		TicketNumber:    "TK-0007",
		Category:        models.TicketCategoryMiner,
		Status:          models.TicketStatusOpen,
		Component:       "Updated after creation",
		MinerIdentifier: &minerID,
	}
	canonicalParams := params
	canonicalParams.MinerIdentifier = &minerID
	requestHash, err := createRepairTicketRequestHash(canonicalParams)
	require.NoError(t, err)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-ticket-1").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-ticket-1").Return(existing, requestHash, nil),
	)

	ticket, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	assert.Same(t, existing, ticket)
}

func TestCreateRepairTicketResetsCreatedFlagWhenTransactionAttemptIsReplayed(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
	siteID := int64(11)
	params := models.CreateParams{
		OrgID:          4,
		IdempotencyKey: "create-ticket-replayed",
		Category:       models.TicketCategoryInfrastructure,
		Component:      "Network",
		SiteID:         &siteID,
	}
	requestHash, err := createRepairTicketRequestHash(params)
	require.NoError(t, err)
	created := &models.RepairTicket{UpdatedAt: testVersion,
		ID:           9,
		OrgID:        4,
		TicketNumber: "TK-0007",
		Category:     models.TicketCategoryInfrastructure,
		Status:       models.TicketStatusOpen,
		Component:    "Network",
		SiteID:       &siteID,
	}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			txCtx := context.WithValue(ctx, txMarker{}, true)
			require.NoError(t, fn(txCtx), "first transaction attempt")
			return fn(txCtx)
		},
	)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-ticket-replayed").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-ticket-replayed").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveLocationContext(txContextMatcher{}, int64(4), &siteID, nil).
			Return(&models.AssetContext{SiteID: &siteID}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), siteID).Return(nil),
		tickets.EXPECT().NextTicketNumber(txContextMatcher{}, int64(4)).Return(int64(7), nil),
		tickets.EXPECT().CreateRepairTicket(txContextMatcher{}, gomock.Any(), "TK-0007").Return(created, nil),
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-ticket-replayed").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-ticket-replayed").Return(created, requestHash, nil),
	)

	ticket, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	assert.Same(t, created, ticket)
}

func TestCreateRepairTicketRejectsIdempotencyKeyReuseWithDifferentRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	minerID := "miner-1"
	params := models.CreateParams{
		OrgID:           4,
		IdempotencyKey:  "create-ticket-1",
		Category:        models.TicketCategoryMiner,
		Component:       "Fan",
		MinerIdentifier: &minerID,
	}
	existing := &models.RepairTicket{UpdatedAt: testVersion,
		ID:              9,
		OrgID:           4,
		TicketNumber:    "TK-0007",
		Category:        models.TicketCategoryMiner,
		Status:          models.TicketStatusOpen,
		Component:       "Fan",
		MinerIdentifier: &minerID,
	}
	originalParams := params
	originalParams.Component = "Hashboard"
	originalRequestHash, err := createRepairTicketRequestHash(originalParams)
	require.NoError(t, err)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-ticket-1").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-ticket-1").Return(existing, originalRequestHash, nil),
	)

	_, err = service.CreateRepairTicket(t.Context(), params)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "%v", err)
}

func TestCreateRepairTicketDerivesMinerContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, buildingID, rackID, assigneeID := int64(11), int64(12), int64(13), int64(14)
	zone, rack, group := "A", "Rack 1", "Hot"
	params := models.CreateParams{OrgID: 4, IdempotencyKey: "create-miner-ticket", Category: models.TicketCategoryMiner, Component: " Hashboard ", Diagnosis: stringPointer(" Board failed "), MinerIdentifier: stringPointer(" miner-1 "), AssigneeUserID: &assigneeID}
	expected := params
	expected.Component = "Hashboard"
	expected.Diagnosis = stringPointer("Board failed")
	expected.MinerIdentifier = stringPointer("miner-1")
	expected.SiteID, expected.BuildingID, expected.RackID = &siteID, &buildingID, &rackID
	expected.Zone, expected.RackLabel, expected.GroupLabel = &zone, &rack, &group
	requestHash, err := createRepairTicketRequestHash(expected)
	require.NoError(t, err)
	expected.CreateRequestHash = requestHash

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-miner-ticket").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-miner-ticket").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveAssignee(txContextMatcher{}, int64(4), assigneeID).Return(&models.Assignee{UserID: assigneeID}, nil),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &siteID, BuildingID: &buildingID, Zone: &zone, RackID: &rackID, RackLabel: &rack, GroupLabel: &group}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), siteID).Return(nil),
		refs.EXPECT().LockBuildingForTicket(txContextMatcher{}, int64(4), buildingID).Return(nil),
		refs.EXPECT().LockRackForTicket(txContextMatcher{}, int64(4), rackID).Return(nil),
		refs.EXPECT().LockMinerForTicket(txContextMatcher{}, int64(4), "miner-1").Return(nil),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &siteID, BuildingID: &buildingID, Zone: &zone, RackID: &rackID, RackLabel: &rack, GroupLabel: &group}, nil),
		tickets.EXPECT().NextTicketNumber(txContextMatcher{}, int64(4)).Return(int64(1), nil),
		tickets.EXPECT().CreateRepairTicket(txContextMatcher{}, expected, "TK-0001").Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 1, OrgID: 4, Component: "Hashboard"}, nil),
	)
	ticket, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, "Hashboard", ticket.Component)
}

func TestCreateRepairTicketRejectsMinerPlacementThatChangesBeforeLocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	oldSiteID, oldBuildingID, oldRackID := int64(11), int64(12), int64(13)
	newSiteID, newBuildingID, newRackID := int64(21), int64(22), int64(23)
	params := models.CreateParams{
		OrgID: 4, IdempotencyKey: "create-moving-miner", Category: models.TicketCategoryMiner,
		Component: "Hashboard", MinerIdentifier: stringPointer("miner-1"),
	}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-moving-miner").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-moving-miner").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &oldSiteID, BuildingID: &oldBuildingID, RackID: &oldRackID}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), oldSiteID).Return(nil),
		refs.EXPECT().LockBuildingForTicket(txContextMatcher{}, int64(4), oldBuildingID).Return(nil),
		refs.EXPECT().LockRackForTicket(txContextMatcher{}, int64(4), oldRackID).Return(nil),
		refs.EXPECT().LockMinerForTicket(txContextMatcher{}, int64(4), "miner-1").Return(nil),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &newSiteID, BuildingID: &newBuildingID, RackID: &newRackID}, nil),
	)

	_, err := service.CreateRepairTicket(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestCreateRepairTicketUsesFinalDescriptiveLabels(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, buildingID, rackID := int64(11), int64(12), int64(13)
	oldGroup, newGroup := "Hot", "Needs review"
	params := models.CreateParams{
		OrgID: 4, IdempotencyKey: "create-moving-group", Category: models.TicketCategoryMiner,
		Component: "Hashboard", MinerIdentifier: stringPointer("miner-1"),
	}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-moving-group").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-moving-group").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &siteID, BuildingID: &buildingID, RackID: &rackID, GroupLabel: &oldGroup}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), siteID).Return(nil),
		refs.EXPECT().LockBuildingForTicket(txContextMatcher{}, int64(4), buildingID).Return(nil),
		refs.EXPECT().LockRackForTicket(txContextMatcher{}, int64(4), rackID).Return(nil),
		refs.EXPECT().LockMinerForTicket(txContextMatcher{}, int64(4), "miner-1").Return(nil),
		refs.EXPECT().ResolveMinerContext(txContextMatcher{}, int64(4), "miner-1").Return(&models.AssetContext{MinerIdentifier: "miner-1", SiteID: &siteID, BuildingID: &buildingID, RackID: &rackID, GroupLabel: &newGroup, RackLabel: &newGroup, Zone: &newGroup}, nil),
		tickets.EXPECT().NextTicketNumber(txContextMatcher{}, int64(4)).Return(int64(1), nil),
		tickets.EXPECT().CreateRepairTicket(txContextMatcher{}, gomock.Any(), "TK-0001").DoAndReturn(func(_ context.Context, created models.CreateParams, _ string) (*models.RepairTicket, error) {
			require.Equal(t, &newGroup, created.GroupLabel)
			require.Equal(t, &newGroup, created.RackLabel)
			require.Equal(t, &newGroup, created.Zone)
			return &models.RepairTicket{UpdatedAt: testVersion, ID: 1}, nil
		}),
	)

	_, err := service.CreateRepairTicket(t.Context(), params)
	require.NoError(t, err)
}

func TestCreateRepairTicketDerivesMinerContextAndRejectsCrossOrgReference(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	inventory := mocks.NewMockInventoryStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, inventory, tx, nil)
	params := models.CreateParams{OrgID: 4, IdempotencyKey: "create-cross-org-ticket", Category: models.TicketCategoryMiner, Component: "Hashboard", MinerIdentifier: stringPointer("miner-1")}
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-cross-org-ticket").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-cross-org-ticket").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveMinerContext(gomock.Any(), int64(4), "miner-1").Return(nil, fleeterror.NewNotFoundError("miner not found")),
	)
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
	requestHash, err := createCommentRequestHash(3, 4, "fixed it")
	require.NoError(t, err)
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().LockTicketCommentCreateKey(txContextMatcher{}, int64(2), "comment-key").Return(nil),
		tickets.EXPECT().GetTicketCommentByIdempotencyKey(txContextMatcher{}, int64(2), "comment-key").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		tickets.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 3, SiteID: &siteID}, nil),
		tickets.EXPECT().CreateTicketComment(txContextMatcher{}, int64(2), int64(3), int64(4), "fixed it", "comment-key", requestHash).Return(&models.TicketComment{ID: 5, Text: "fixed it"}, nil),
	)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		require.NotNil(t, event.SiteID)
		assert.Equal(t, siteID, *event.SiteID)
		return nil
	})
	comment, err := service.CreateComment(t.Context(), 2, 3, 4, "tech", "  fixed it  ", "comment-key")
	require.NoError(t, err)
	assert.Equal(t, "fixed it", comment.Text)

	_, err = service.CreateComment(t.Context(), 2, 3, 4, "tech", string(make([]rune, 4097)), "long-comment-key")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateCommentReturnsMatchingReplayWithoutDuplicateActivity(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, activity.NewService(activityStore))
	siteID := int64(11)
	existing := &models.TicketComment{ID: 5, OrgID: 2, TicketID: 3, UserID: 4, Text: "fixed it"}
	requestHash, err := createCommentRequestHash(3, 4, "fixed it")
	require.NoError(t, err)

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().LockTicketCommentCreateKey(txContextMatcher{}, int64(2), "comment-retry-key").Return(nil),
		tickets.EXPECT().GetTicketCommentByIdempotencyKey(txContextMatcher{}, int64(2), "comment-retry-key").Return(existing, requestHash, nil),
		tickets.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 3, SiteID: &siteID}, nil),
	)

	comment, err := service.CreateComment(t.Context(), 2, 3, 4, "tech", "fixed it", "comment-retry-key")
	require.NoError(t, err)
	assert.Same(t, existing, comment)
	assert.True(t, comment.AuthoredByCaller)
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
	params := models.BulkCloseParams{
		OrgID: 2, TicketIDs: []int64{4, 3, 3}, Resolution: models.TicketResolutionRepaired, RepairLocation: models.RepairLocationOnRack,
		ExpectedVersions: map[int64]time.Time{3: testVersion, 4: testVersion},
	}
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 3, Category: models.TicketCategoryMiner, Status: models.TicketStatusOpen}, nil),
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(4)).Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 4, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen}, nil),
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

func TestBulkCloseRequiresExpectedVersionForEveryTicket(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(
		mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl),
		mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil,
	)

	for _, tc := range []struct {
		name     string
		versions map[int64]time.Time
	}{
		{"missing", nil},
		{"extra", map[int64]time.Time{3: testVersion, 4: testVersion}},
		{"wrong ticket", map[int64]time.Time{4: testVersion}},
		{"zero timestamp", map[int64]time.Time{3: {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.BulkClose(t.Context(), models.BulkCloseParams{
				OrgID: 2, TicketIDs: []int64{3}, ExpectedVersions: tc.versions, Resolution: models.TicketResolutionDeferred,
			})
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
			assert.ErrorContains(t, err, "expected_versions")
		})
	}
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
			{UpdatedAt: testVersion, ID: 3, SiteID: &siteA, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen},
			{UpdatedAt: testVersion, ID: 4, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen},
			{UpdatedAt: testVersion, ID: 5, SiteID: &siteB, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusOpen},
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
		_, err := service.BulkUpdateStatus(t.Context(), 2, []int64{5, 3, 4}, models.TicketStatusInProgress, map[int64]time.Time{
			3: testVersion, 4: testVersion, 5: testVersion,
		})
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
		_, err := service.BulkAssign(t.Context(), 2, []int64{5, 3, 4}, &assigneeID, map[int64]time.Time{3: testVersion, 4: testVersion, 5: testVersion})
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
		_, err := service.BulkMarkUrgent(t.Context(), 2, []int64{5, 3, 4}, map[int64]time.Time{5: testVersion, 3: testVersion, 4: testVersion})
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
		_, err := service.BulkClose(t.Context(), models.BulkCloseParams{
			OrgID: 2, TicketIDs: []int64{5, 3, 4}, Resolution: models.TicketResolutionDeferred,
			ExpectedVersions: map[int64]time.Time{
				3: testVersion, 4: testVersion, 5: testVersion,
			},
		})
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

	params := models.UpdateParams{ExpectedUpdatedAt: testVersion, OrgID: 2, ID: 3, PartsSelection: &selection}
	current := &models.RepairTicket{UpdatedAt: testVersion, ID: 3, OrgID: 2, SiteID: &ticketSiteID, Category: models.TicketCategoryMiner, Status: models.TicketStatusOpen}

	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil),
		tickets.EXPECT().ListTicketParts(gomock.Any(), int64(2), int64(3)).Return(nil, nil),
		inventory.EXPECT().GetForUpdate(gomock.Any(), int64(2), int64(7)).Return(&inventorymodels.InventoryPart{ID: 7, OrgID: 2, SiteID: &partSiteID}, nil),
	)

	_, err := service.UpdateRepairTicket(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
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

	_, err := service.BulkUpdateStatus(
		t.Context(), 2, []int64{3}, models.TicketStatusSentToVendor,
		map[int64]time.Time{3: testVersion},
	)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestBulkAssignRejectsCompletedTicket(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 3, Status: models.TicketStatusCompleted}, nil)

	_, err := service.BulkAssign(t.Context(), 2, []int64{3}, nil, map[int64]time.Time{3: testVersion})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestBulkMarkUrgentRejectsCompletedTicket(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{UpdatedAt: testVersion, ID: 3, Status: models.TicketStatusCompleted}, nil)

	_, err := service.BulkMarkUrgent(t.Context(), 2, []int64{3}, map[int64]time.Time{3: testVersion})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestBulkCloseRejectsCompletedTicketWithDifferentDetails(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	params := models.BulkCloseParams{
		OrgID: 2, TicketIDs: []int64{3}, Resolution: models.TicketResolutionDeferred,
		ExpectedVersions: map[int64]time.Time{3: testVersion},
	}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	tickets.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{UpdatedAt: testVersion,
		ID: 3, Category: models.TicketCategoryInfrastructure, Status: models.TicketStatusCompleted,
		Resolution: models.TicketResolutionRepaired,
	}, nil)

	_, err := service.BulkClose(t.Context(), params)

	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	assert.ErrorContains(t, err, "terminal")
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
