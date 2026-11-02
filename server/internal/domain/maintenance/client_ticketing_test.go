package maintenance

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCompletedTicketCanExplicitlyReopen(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	status := models.TicketStatusOpen
	params := models.UpdateParams{OrgID: 2, ID: 3, ExpectedUpdatedAt: testVersion, Status: &status}
	current := &models.RepairTicket{ID: 3, OrgID: 2, UpdatedAt: testVersion, Status: models.TicketStatusCompleted}
	updated := *current
	updated.Status = status
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	store.EXPECT().GetRepairTicket(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	store.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	store.EXPECT().UpdateRepairTicket(gomock.Any(), params).Return(&updated, nil)
	got, err := service.UpdateRepairTicket(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, models.TicketStatusOpen, got.Status)
}

func TestCloseRequiresExplicitVisibleResolutionNote(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	status := models.TicketStatusCompleted
	resolution := models.TicketResolutionDeferred
	current := &models.RepairTicket{ID: 3, OrgID: 2, UpdatedAt: testVersion, Status: models.TicketStatusOpen, Notes: stringPointer("old private operational note")}
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	store.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(current, nil)
	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3, ExpectedUpdatedAt: testVersion, Status: &status, Resolution: &resolution, Notes: stringPointer("Private operational update")})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	require.ErrorContains(t, err, "resolution note")
}

func TestBulkCloseRequiresVisibleResolutionNote(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewService(mocks.NewMockMaintenanceStore(ctrl), mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), mocks.NewMockTransactor(ctrl), nil)
	_, err := service.BulkClose(t.Context(), models.BulkCloseParams{OrgID: 2, TicketIDs: []int64{3}, ExpectedVersions: map[int64]time.Time{3: testVersion}, Resolution: models.TicketResolutionDeferred})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	require.ErrorContains(t, err, "resolution note")
}

func TestTargetEvidenceRejectsWrongModelModuleReplacement(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	replacementID := int64(11)
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	store.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen, UpdatedAt: testVersion, Targets: []models.TicketAsset{{Kind: models.TicketCategoryModule, ID: 10, Model: "BM1368"}}}, nil)
	refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, replacementID).Return(&models.TicketAsset{ID: 11, Kind: models.TicketCategoryModule, Model: "BM1370"}, nil)
	_, err := service.UpdateTicketTarget(t.Context(), models.UpdateTargetParams{OrgID: 2, TicketID: 3, Kind: models.TicketCategoryModule, AssetID: 10, ExpectedUpdatedAt: testVersion, ReplacementModuleID: &replacementID})
	require.ErrorContains(t, err, "same actual hardware model")
}

func TestTargetEvidenceRejectsSamePhysicalBoardReplacement(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, mocks.NewMockMaintenanceReferenceStore(ctrl), mocks.NewMockInventoryStore(ctrl), tx, nil)
	boardID := int64(10)
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	store.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen, UpdatedAt: testVersion, Targets: []models.TicketAsset{{Kind: models.TicketCategoryHashboard, ID: 10}}}, nil)
	_, err := service.UpdateTicketTarget(t.Context(), models.UpdateTargetParams{OrgID: 2, TicketID: 3, Kind: models.TicketCategoryHashboard, AssetID: boardID, ExpectedUpdatedAt: testVersion, ReplacementHashboardID: &boardID})
	require.ErrorContains(t, err, "different physical hashboard")
}

func TestCreateRejectsTargetsAcrossSitesBeforePersisting(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	site1, site2 := int64(100), int64(101)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	store.EXPECT().LockRepairTicketCreateKey(gomock.Any(), int64(2), "cross-site").Return(nil)
	store.EXPECT().GetRepairTicketByIdempotencyKey(gomock.Any(), int64(2), "cross-site").Return(nil, "", fleeterror.NewNotFoundError("new key"))
	refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, int64(10)).Return(&models.TicketAsset{ID: 10, SiteID: &site1}, nil)
	refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, int64(11)).Return(&models.TicketAsset{ID: 11, SiteID: &site2}, nil)
	_, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: 2, IdempotencyKey: "cross-site", Category: models.TicketCategoryModule, Component: "Power", Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: 10}, {Kind: models.TicketCategoryModule, ID: 11}}})
	require.ErrorContains(t, err, "same site")
}

func TestCommentIdempotencyIncludesVisibility(t *testing.T) {
	shared, err := createCommentRequestHash(3, 4, "fixed", models.CommentScope{Internal: false})
	require.NoError(t, err)
	internal, err := createCommentRequestHash(3, 4, "fixed", models.CommentScope{Internal: true})
	require.NoError(t, err)
	private, err := createCommentRequestHash(3, 4, "fixed", models.CommentScope{AccountID: "client-uuid"})
	require.NoError(t, err)
	require.NotEqual(t, shared, internal)
	require.NotEqual(t, shared, private)
	require.NotEqual(t, private, internal)
}

func TestCreateRechecksTargetSiteAfterPlacementLocks(t *testing.T) {
	for _, kind := range []models.TicketCategory{models.TicketCategoryModule, models.TicketCategoryHashboard, models.TicketCategoryMiner} {
		t.Run(fmt.Sprintf("kind=%d", kind), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockMaintenanceStore(ctrl)
			refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
			tx := mocks.NewMockTransactor(ctrl)
			service := NewService(store, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
			site, deniedSite, parent := int64(100), int64(101), int64(20)
			selections := []models.TicketAssetSelection{{Kind: kind, ID: 10}}
			if kind == models.TicketCategoryMiner {
				selections = append(selections, models.TicketAssetSelection{Kind: kind, ID: 11})
			}
			if kind == models.TicketCategoryHashboard {
				selections[0].ParentModuleID = &parent
			}
			tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
			store.EXPECT().LockRepairTicketCreateKey(gomock.Any(), int64(2), "moving-target").Return(nil)
			store.EXPECT().GetRepairTicketByIdempotencyKey(gomock.Any(), int64(2), "moving-target").Return(nil, "", fleeterror.NewNotFoundError("new key"))
			calls := []any{}
			for _, selection := range selections {
				calls = append(calls, refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), kind, selection.ID).Return(&models.TicketAsset{ID: selection.ID, SiteID: &site, ModuleID: &parent}, nil))
			}
			calls = append(calls,
				refs.EXPECT().LockSiteForTicket(gomock.Any(), int64(2), site).Return(nil),
				refs.EXPECT().LockTicketAssetPlacements(gomock.Any(), int64(2), selections).Return(nil),
				refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), kind, int64(10)).Return(&models.TicketAsset{ID: 10, SiteID: &deniedSite, ModuleID: &parent}, nil),
			)
			gomock.InOrder(calls...)
			ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{site}})
			_, _, err := service.CreateRepairTicket(ctx, models.CreateParams{OrgID: 2, IdempotencyKey: "moving-target", Category: kind, Component: "Power", Targets: selections})
			require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
		})
	}
}

func TestReplacementModuleModelRechecksLockedSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	site, deniedSite, original, replacement := int64(100), int64(101), int64(10), int64(11)
	model := "Actual model"
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	store.EXPECT().GetRepairTicketForUpdate(gomock.Any(), int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen, UpdatedAt: testVersion, SiteID: &site, Targets: []models.TicketAsset{{Kind: models.TicketCategoryModule, ID: original, ModuleID: &original, Model: model}}}, nil)
	refs.EXPECT().LockModuleModelSite(gomock.Any(), int64(2), replacement).Return(&deniedSite, nil)
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{site}})
	_, err := service.UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: 2, TicketID: 3, Kind: models.TicketCategoryModule, AssetID: original, ExpectedUpdatedAt: testVersion, ReplacementModuleID: &replacement, ReplacementModuleModel: &model})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
}

func TestClientClosureChecksSafeguardsBeforeReleasingParts(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	account, status, resolution := int64(8), models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(runResultTx)
	gomock.InOrder(
		store.EXPECT().GetRepairTicketForUpdate(txContextMatcher{}, int64(2), int64(3)).Return(&models.RepairTicket{ID: 3, OrgID: 2, Status: models.TicketStatusOpen, UpdatedAt: testVersion}, nil),
		refs.EXPECT().CanAccountCloseTicket(txContextMatcher{}, int64(2), int64(3), account).Return(false, nil),
	)
	_, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: 2, ID: 3, ExpectedUpdatedAt: testVersion, ClosingAccountID: &account, Status: &status, Resolution: &resolution, ResolutionNote: stringPointer("Client no longer needs repair")})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}

func TestCreateRejectsSplitTargetSitesAfterPlacementLocks(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockMaintenanceStore(ctrl)
	refs := mocks.NewMockMaintenanceReferenceStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	service := NewService(store, refs, mocks.NewMockInventoryStore(ctrl), tx, nil)
	site, destination := int64(100), int64(101)
	selections := []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: 10}, {Kind: models.TicketCategoryModule, ID: 11}}
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(runTx)
	store.EXPECT().LockRepairTicketCreateKey(gomock.Any(), int64(2), "split-targets").Return(nil)
	store.EXPECT().GetRepairTicketByIdempotencyKey(gomock.Any(), int64(2), "split-targets").Return(nil, "", fleeterror.NewNotFoundError("new key"))
	gomock.InOrder(
		refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, int64(10)).Return(&models.TicketAsset{ID: 10, SiteID: &site}, nil),
		refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, int64(11)).Return(&models.TicketAsset{ID: 11, SiteID: &site}, nil),
		refs.EXPECT().LockSiteForTicket(gomock.Any(), int64(2), site).Return(nil),
		refs.EXPECT().LockTicketAssetPlacements(gomock.Any(), int64(2), selections).Return(nil),
		refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, int64(10)).Return(&models.TicketAsset{ID: 10, SiteID: &site}, nil),
		refs.EXPECT().ResolveTicketAsset(gomock.Any(), int64(2), models.TicketCategoryModule, int64(11)).Return(&models.TicketAsset{ID: 11, SiteID: &destination}, nil),
	)
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{site, destination}})
	_, _, err := service.CreateRepairTicket(ctx, models.CreateParams{OrgID: 2, IdempotencyKey: "split-targets", Category: models.TicketCategoryModule, Component: "Power", Targets: selections})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
}
