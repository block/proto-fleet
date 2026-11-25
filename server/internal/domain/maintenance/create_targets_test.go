package maintenance

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestCreateRepairTicketRejectsBoardThatMovesModulesBeforePlacementLocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, boardID, oldModuleID, newModuleID := int64(11), int64(20), int64(30), int64(31)
	selection := models.TicketAssetSelection{Kind: models.TicketCategoryHashboard, ID: boardID, ParentModuleID: &oldModuleID}
	params := models.CreateParams{
		OrgID: 4, IdempotencyKey: "create-moving-board", Category: models.TicketCategoryModule,
		Component: "Hashboard", Targets: []models.TicketAssetSelection{selection},
	}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-moving-board").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-moving-board").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveTicketAsset(txContextMatcher{}, int64(4), models.TicketCategoryHashboard, boardID).
			Return(&models.TicketAsset{Kind: models.TicketCategoryHashboard, ID: boardID, ModuleID: &oldModuleID, SiteID: &siteID}, nil),
		refs.EXPECT().LockSiteForTicket(txContextMatcher{}, int64(4), siteID).Return(nil),
		refs.EXPECT().LockTicketAssetPlacements(txContextMatcher{}, int64(4), []models.TicketAssetSelection{selection}).Return(nil),
		refs.EXPECT().ResolveTicketAsset(txContextMatcher{}, int64(4), models.TicketCategoryHashboard, boardID).
			Return(&models.TicketAsset{Kind: models.TicketCategoryHashboard, ID: boardID, ModuleID: &newModuleID, SiteID: &siteID}, nil),
	)
	// No NextTicketNumber or CreateRepairTicket expectations: gomock fails the
	// test if the service attempts to create the ticket.

	ticket, createdNew, err := service.CreateRepairTicket(t.Context(), params)
	assert.Nil(t, ticket)
	assert.False(t, createdNew)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "hashboard parent must match its current installation")
}

func TestCreateRepairTicketRejectsModuleWithBoardFromDifferentModule(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, moduleID, otherModuleID, boardID := int64(11), int64(30), int64(31), int64(20)
	params := models.CreateParams{
		OrgID: 4, IdempotencyKey: "create-module-and-foreign-board", Category: models.TicketCategoryModule,
		Component: "Hashboard", Targets: []models.TicketAssetSelection{
			{Kind: models.TicketCategoryModule, ID: moduleID},
			{Kind: models.TicketCategoryHashboard, ID: boardID, ParentModuleID: &otherModuleID},
		},
	}

	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	gomock.InOrder(
		tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-module-and-foreign-board").Return(nil),
		tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-module-and-foreign-board").Return(nil, "", fleeterror.NewNotFoundError("not found")),
		refs.EXPECT().ResolveTicketAsset(txContextMatcher{}, int64(4), models.TicketCategoryModule, moduleID).
			Return(&models.TicketAsset{Kind: models.TicketCategoryModule, ID: moduleID, SiteID: &siteID}, nil),
		refs.EXPECT().ResolveTicketAsset(txContextMatcher{}, int64(4), models.TicketCategoryHashboard, boardID).
			Return(&models.TicketAsset{Kind: models.TicketCategoryHashboard, ID: boardID, ModuleID: &otherModuleID, SiteID: &siteID}, nil),
	)

	_, createdNew, err := service.CreateRepairTicket(t.Context(), params)
	assert.False(t, createdNew)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	assert.ErrorContains(t, err, "equipment tickets must concern one parent miner or module")
}

func TestCreateRepairTicketRejectsMinerWithModuleTarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	tickets := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(tickets, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	siteID, moduleID, minerID := int64(11), int64(30), int64(5)

	for _, tc := range []struct {
		name     string
		category models.TicketCategory
		targets  []models.TicketAssetSelection
	}{
		{
			name:     "module ticket with trailing miner",
			category: models.TicketCategoryModule,
			targets: []models.TicketAssetSelection{
				{Kind: models.TicketCategoryModule, ID: moduleID},
				{Kind: models.TicketCategoryMiner, ID: minerID},
			},
		},
		{
			name:     "miner ticket with trailing module",
			category: models.TicketCategoryMiner,
			targets: []models.TicketAssetSelection{
				{Kind: models.TicketCategoryMiner, ID: minerID},
				{Kind: models.TicketCategoryModule, ID: moduleID},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := models.CreateParams{
				OrgID: 4, IdempotencyKey: "create-miner-and-module", Category: tc.category,
				Component: "Hashboard", Targets: tc.targets,
			}
			tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
			gomock.InOrder(
				tickets.EXPECT().LockRepairTicketCreateKey(txContextMatcher{}, int64(4), "create-miner-and-module").Return(nil),
				tickets.EXPECT().GetRepairTicketByIdempotencyKey(txContextMatcher{}, int64(4), "create-miner-and-module").Return(nil, "", fleeterror.NewNotFoundError("not found")),
				refs.EXPECT().ResolveTicketAsset(txContextMatcher{}, int64(4), tc.targets[0].Kind, tc.targets[0].ID).
					Return(&models.TicketAsset{Kind: tc.targets[0].Kind, ID: tc.targets[0].ID, SiteID: &siteID}, nil),
			)

			_, createdNew, err := service.CreateRepairTicket(t.Context(), params)
			assert.False(t, createdNew)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
			assert.ErrorContains(t, err, "target kind must match ticket category")
		})
	}
}
