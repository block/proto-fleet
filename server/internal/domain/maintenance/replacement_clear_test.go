package maintenance

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTargetEvidenceRejectsInvalidReplacementClears(t *testing.T) {
	replacement := int64(11)
	for _, tc := range []struct {
		name   string
		params models.UpdateTargetParams
	}{
		{"module set and clear", models.UpdateTargetParams{Kind: models.TicketCategoryModule, ClearReplacementModule: true, ReplacementModuleID: &replacement}},
		{"replacement model and clear", models.UpdateTargetParams{Kind: models.TicketCategoryModule, ClearReplacementModule: true, ReplacementModuleModel: stringPointer("BM1368")}},
		{"board set and clear", models.UpdateTargetParams{Kind: models.TicketCategoryHashboard, ClearReplacementHashboard: true, ReplacementHashboardID: &replacement}},
		{"module clear on board", models.UpdateTargetParams{Kind: models.TicketCategoryHashboard, ClearReplacementModule: true}},
		{"board clear on module", models.UpdateTargetParams{Kind: models.TicketCategoryModule, ClearReplacementHashboard: true}},
		{"module clear on miner", models.UpdateTargetParams{Kind: models.TicketCategoryMiner, ClearReplacementModule: true}},
		{"board clear on miner", models.UpdateTargetParams{Kind: models.TicketCategoryMiner, ClearReplacementHashboard: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			service := NewService(mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil)
			tc.params.ExpectedUpdatedAt = testVersion
			_, err := service.UpdateTicketTarget(t.Context(), tc.params)
			require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
		})
	}
}

func TestTargetEvidenceAllowsReplacementClearWithoutOtherEvidence(t *testing.T) {
	for _, kind := range []models.TicketCategory{models.TicketCategoryModule, models.TicketCategoryHashboard} {
		t.Run(fmt.Sprintf("kind=%d", kind), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockMaintenanceStore(ctrl)
			tx := mocks.NewMockTransactor(ctrl)
			service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
			params := models.UpdateTargetParams{OrgID: 2, TicketID: 3, Kind: kind, AssetID: 10, ExpectedUpdatedAt: testVersion,
				ClearReplacementModule: kind == models.TicketCategoryModule, ClearReplacementHashboard: kind == models.TicketCategoryHashboard}
			ticket := &models.RepairTicket{ID: 3, OrgID: 2, UpdatedAt: testVersion, Status: models.TicketStatusOpen, Targets: []models.TicketAsset{{ID: 10, Kind: kind, Model: "BM1368"}}}
			tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
			gomock.InOrder(
				store.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(ticket, nil),
				store.EXPECT().UpdateTicketTarget(txContextMatcher{}, params).Return(nil),
				store.EXPECT().GetRepairTicket(txContextMatcher{}, int64(2), int64(3)).Return(ticket, nil),
			)
			_, err := service.UpdateTicketTarget(t.Context(), params)
			require.NoError(t, err)
		})
	}
}
