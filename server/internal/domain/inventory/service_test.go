package inventory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestListPartsReturnsFilteredTotalAndOnlyEmitsCursorWhenAnotherPageExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	service := NewService(store, nil, nil)
	filter := models.ListFilter{OrgID: 42, Types: []string{"fan"}, Limit: 2}

	store.EXPECT().Count(gomock.Any(), filter).Return(int32(3), nil)
	store.EXPECT().List(gomock.Any(), models.ListFilter{OrgID: 42, Types: []string{"fan"}, Limit: 3}).Return(
		[]models.InventoryPart{{ID: 9}, {ID: 7}, {ID: 5}}, nil,
	)

	page, err := service.ListParts(t.Context(), filter)
	require.NoError(t, err)
	assert.Equal(t, int32(3), page.TotalCount)
	require.Len(t, page.Parts, 2)
	require.NotNil(t, page.NextCursorID)
	assert.Equal(t, int64(7), *page.NextCursorID)

	cursor := int64(7)
	finalFilter := models.ListFilter{OrgID: 42, Types: []string{"fan"}, CursorID: &cursor, Limit: 2}
	store.EXPECT().Count(gomock.Any(), finalFilter).Return(int32(3), nil)
	store.EXPECT().List(gomock.Any(), models.ListFilter{OrgID: 42, Types: []string{"fan"}, CursorID: &cursor, Limit: 3}).Return(
		[]models.InventoryPart{{ID: 5}}, nil,
	)

	finalPage, err := service.ListParts(t.Context(), finalFilter)
	require.NoError(t, err)
	assert.Nil(t, finalPage.NextCursorID)
}

func TestCreatePartCanonicalizesDisplayFieldsBeforePersistence(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	service := NewService(store, nil, nil)
	manufacturer := " Proto "
	partNumber := " FAN-1 "
	binLocation := " A-12 "
	params := models.CreateParams{
		OrgID: 42, Name: " Fan ", Type: " Cooling ", Manufacturer: &manufacturer,
		PartNumber: &partNumber, BinLocation: &binLocation,
	}
	trimmedManufacturer := "Proto"
	trimmedPartNumber := "FAN-1"
	trimmedBinLocation := "A-12"
	canonical := models.CreateParams{
		OrgID: 42, Name: "Fan", Type: "Cooling", Manufacturer: &trimmedManufacturer,
		PartNumber: &trimmedPartNumber, BinLocation: &trimmedBinLocation,
	}
	created := &models.InventoryPart{ID: 7, OrgID: 42, Name: "Fan", Type: "Cooling"}

	store.EXPECT().Create(gomock.Any(), canonical).Return(created, nil)

	part, err := service.CreatePart(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, created, part)
}

func TestCreatePartLocksAssignedSiteInTheCreateTransaction(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(store, transactor, activity.NewService(activityStore))
	siteID := int64(11)
	params := models.CreateParams{OrgID: 42, Name: "Fan", Type: "cooling", SiteID: &siteID}

	transactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) (any, error)) (any, error) {
			return action(context.WithValue(ctx, inventoryTxMarker{}, true))
		},
	)
	store.EXPECT().LockSites(inventoryTxContextMatcher{}, int64(42), []int64{siteID}).Return(nil)
	store.EXPECT().Create(inventoryTxContextMatcher{}, params).Return(&models.InventoryPart{ID: 7, OrgID: 42, SiteID: &siteID, Name: "Fan", Type: "cooling"}, nil)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		require.NotNil(t, event.SiteID)
		assert.Equal(t, siteID, *event.SiteID)
		return nil
	})

	part, err := service.CreatePart(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, int64(7), part.ID)
}

func TestUpdatePartCanAssignPreviouslyUnassignedStockToASite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	siteID := int64(11)
	params := models.UpdateParams{
		OrgID: 42, ID: 7, SiteID: &siteID, Reason: models.AdjustmentReasonCycleCount,
	}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().LockSites(gomock.Any(), int64(42), []int64{siteID}).Return(nil)
	store.EXPECT().GetForUpdate(gomock.Any(), int64(42), int64(7)).Return(
		&models.InventoryPart{ID: 7, OrgID: 42, Name: "Fan", Allocated: 0}, nil,
	)
	store.EXPECT().Update(gomock.Any(), params).Return(
		&models.InventoryPart{ID: 7, OrgID: 42, Name: "Fan", SiteID: &siteID}, nil,
	)

	part, err := service.UpdatePart(t.Context(), params)
	require.NoError(t, err)
	require.NotNil(t, part.SiteID)
	assert.Equal(t, siteID, *part.SiteID)
}

func TestUpdatePartRejectsSiteTransferWhileStockIsAllocated(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	oldSiteID := int64(10)
	newSiteID := int64(11)
	params := models.UpdateParams{
		OrgID: 42, ID: 7, SiteID: &newSiteID, Reason: models.AdjustmentReasonCycleCount,
	}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().LockSites(gomock.Any(), int64(42), []int64{newSiteID}).Return(nil)
	store.EXPECT().GetForUpdate(gomock.Any(), int64(42), int64(7)).Return(
		&models.InventoryPart{ID: 7, OrgID: 42, SiteID: &oldSiteID, Name: "Fan", Allocated: 1}, nil,
	)

	_, err := service.UpdatePart(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "allocated stock must not move between sites: %v", err)
}

func TestInventoryParseCsvPreviewResolvesSitesAndReportsErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	service := NewService(store, nil, nil)
	const orgID = int64(42)

	store.EXPECT().PartExistsBySiteAndName(gomock.Any(), orgID, gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
	store.EXPECT().ResolveSiteByName(gomock.Any(), orgID, "Repair Depot").Return(int64(7), nil)
	store.EXPECT().ResolveSiteByName(gomock.Any(), orgID, "Missing Site").Return(int64(0), fleeterror.NewNotFoundError("missing"))

	rows, err := service.ParseCsvPreview(t.Context(), orgID, []byte(strings.Join([]string{
		"name,type,manufacturer,part_number,site_name,on_hand,reorder_point,bin_location",
		"Hashboard,board,Proto,HB-1,Repair Depot,4,1,A-1",
		"PSU,power,Proto,PSU-1,Missing Site,2,1,B-1",
	}, "\n")))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Empty(t, rows[0].Error)
	assert.Equal(t, 2, rows[0].RowNumber)
	assert.Contains(t, rows[1].Error, "Missing Site")
}

func TestInventoryParseCsvPreviewFlagsExistingPartAtResolvedSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	service := NewService(store, nil, nil)
	const orgID = int64(42)
	const siteID = int64(7)

	store.EXPECT().ResolveSiteByName(gomock.Any(), orgID, "Repair Depot").Return(siteID, nil)
	store.EXPECT().PartExistsBySiteAndName(gomock.Any(), orgID, gomock.Any(), "Hashboard").DoAndReturn(
		func(_ context.Context, _ int64, resolvedSiteID *int64, _ string) (bool, error) {
			require.NotNil(t, resolvedSiteID)
			assert.Equal(t, siteID, *resolvedSiteID)
			return true, nil
		},
	)

	rows, err := service.ParseCsvPreview(t.Context(), orgID, []byte(strings.Join([]string{
		"name,type,site_name,on_hand",
		"Hashboard,board,Repair Depot,4",
	}, "\n")))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Contains(t, rows[0].Error, "already exists")
}

func TestConfirmCsvImportRejectsAnyInvalidRowWithoutWrites(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	const orgID = int64(42)

	store.EXPECT().PartExistsBySiteAndName(gomock.Any(), orgID, gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
	store.EXPECT().ResolveSiteByName(gomock.Any(), orgID, "Unknown").Return(int64(0), fleeterror.NewNotFoundError("missing"))
	data := []byte(strings.Join([]string{
		"name,type,site_name,on_hand",
		"Fan,fan,,2",
		"PSU,power,Unknown,1",
	}, "\n"))

	created, err := service.ConfirmCsvImport(t.Context(), orgID, data)
	assert.Zero(t, created)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "any invalid row must reject the complete import: %v", err)
}

func TestConfirmCsvImportResolvesEverySiteInsideOrganization(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(store, transactor, activity.NewService(activityStore))
	const orgID = int64(42)
	const siteID = int64(7)

	store.EXPECT().PartExistsBySiteAndName(gomock.Any(), orgID, gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
	store.EXPECT().ResolveSiteByName(gomock.Any(), orgID, "Repair Depot").Return(siteID, nil)
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().LockSites(gomock.Any(), orgID, []int64{siteID}).Return(nil)
	store.EXPECT().BulkCreate(gomock.Any(), orgID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ int64, rows []models.ResolvedCsvRow) (int32, error) {
			require.Len(t, rows, 2)
			require.NotNil(t, rows[0].SiteID)
			assert.Equal(t, siteID, *rows[0].SiteID)
			assert.Nil(t, rows[1].SiteID)
			assert.Equal(t, "Proto", *rows[0].Manufacturer)
			assert.Equal(t, "HB-1", *rows[0].PartNumber)
			return 2, nil
		},
	)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		assert.True(t, event.MultiSite)
		assert.Equal(t, []int64{siteID}, event.MemberSiteIDs)
		assert.True(t, event.TouchesUnassigned)
		return nil
	})

	data := []byte(strings.Join([]string{
		"name,type,manufacturer,part_number,site_name,on_hand,reorder_point,bin_location",
		"Hashboard,board,Proto,HB-1,Repair Depot,4,1,A-1",
		"PSU,power,,,,2,1,B-1",
	}, "\n"))
	created, err := service.ConfirmCsvImport(t.Context(), orgID, data)
	require.NoError(t, err)
	assert.Equal(t, int32(2), created)
}

func TestInventoryCsvRejectsMoreThanMaximumRowsInsteadOfTruncating(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	store.EXPECT().PartExistsBySiteAndName(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
	service := NewService(store, nil, nil)

	var csv strings.Builder
	csv.WriteString("name,type\n")
	for i := range maxCsvPreviewRows + 1 {
		fmt.Fprintf(&csv, "Part %d,fan\n", i)
	}
	rows, err := service.ParseCsvPreview(t.Context(), 1, []byte(csv.String()))
	assert.Nil(t, rows)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "oversized row count must be rejected: %v", err)
}

func TestInventoryCsvRejectsDuplicateSiteAndName(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	service := NewService(store, nil, nil)
	store.EXPECT().PartExistsBySiteAndName(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
	store.EXPECT().ResolveSiteByName(gomock.Any(), int64(1), "Depot").Times(2).Return(int64(5), nil)

	rows, err := service.ParseCsvPreview(t.Context(), 1, []byte("name,type,site_name\nFan,fan,Depot\n fan ,fan,Depot\n"))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Empty(t, rows[0].Error)
	assert.Contains(t, rows[1].Error, "duplicate")
}

func TestUpdatePartAuditsBeforeAndAfter(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(store, transactor, activity.NewService(activityStore))
	const orgID = int64(42)
	const partID = int64(9)

	onHand := int32(8)
	expectedOnHand := int32(10)
	reorderPoint := int32(3)
	params := models.UpdateParams{
		OrgID: orgID, ID: partID, OnHand: &onHand, ExpectedOnHand: &expectedOnHand, ReorderPoint: &reorderPoint,
		Reason: models.AdjustmentReasonCycleCount,
	}
	siteID := int64(11)
	oldPart := &models.InventoryPart{ID: partID, OrgID: orgID, SiteID: &siteID, Name: "Fan", OnHand: 10, Allocated: 2, ReorderPoint: 1}
	newPart := &models.InventoryPart{ID: partID, OrgID: orgID, SiteID: &siteID, Name: "Fan", OnHand: 8, Allocated: 2, ReorderPoint: 3}
	txComplete := false

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error {
			err := action(ctx)
			txComplete = err == nil
			return err
		},
	)
	store.EXPECT().GetForUpdate(gomock.Any(), orgID, partID).Return(oldPart, nil)
	store.EXPECT().Update(gomock.Any(), params).Return(newPart, nil)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event *activitymodels.Event) error {
			assert.True(t, txComplete, "activity must be emitted after transaction commit")
			require.NotNil(t, event.SiteID)
			assert.Equal(t, siteID, *event.SiteID)
			assert.Equal(t, int32(10), event.Metadata["old_on_hand"])
			assert.Equal(t, int32(8), event.Metadata["new_on_hand"])
			assert.Equal(t, int32(1), event.Metadata["old_reorder_point"])
			assert.Equal(t, int32(3), event.Metadata["new_reorder_point"])
			assert.Equal(t, int16(models.AdjustmentReasonCycleCount), event.Metadata["adjustment_reason"])
			return nil
		},
	)

	part, err := service.UpdatePart(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, newPart, part)
}

func TestUpdatePartRetryDoesNotWriteOrEmitActivity(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(store, transactor, activity.NewService(activityStore))
	const orgID = int64(42)
	const partID = int64(9)
	reorderPoint := int32(3)
	params := models.UpdateParams{
		OrgID: orgID, ID: partID, ReorderPoint: &reorderPoint, Reason: models.AdjustmentReasonCycleCount,
	}
	current := &models.InventoryPart{ID: partID, OrgID: orgID, Name: "Fan", OnHand: 8, ReorderPoint: reorderPoint}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().GetForUpdate(gomock.Any(), orgID, partID).Return(current, nil)

	part, err := service.UpdatePart(t.Context(), params)

	require.NoError(t, err)
	assert.Equal(t, current, part)
}

func TestDeletePartScopesActivityToPersistedSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	activityStore := mocks.NewMockActivityStore(ctrl)
	service := NewService(store, transactor, activity.NewService(activityStore))
	siteID := int64(11)
	part := &models.InventoryPart{ID: 7, OrgID: 42, SiteID: &siteID, Name: "Fan"}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().GetForUpdate(gomock.Any(), int64(42), int64(7)).Return(part, nil)
	store.EXPECT().SoftDelete(gomock.Any(), int64(42), int64(7)).Return(int64(1), nil)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
		require.NotNil(t, event.SiteID)
		assert.Equal(t, siteID, *event.SiteID)
		return nil
	})

	require.NoError(t, service.DeletePart(t.Context(), 42, 7))
}

type inventoryTxMarker struct{}

type inventoryTxContextMatcher struct{}

func (inventoryTxContextMatcher) Matches(value any) bool {
	ctx, ok := value.(context.Context)
	return ok && ctx.Value(inventoryTxMarker{}) == true
}

func (inventoryTxContextMatcher) String() string { return "inventory transaction-bound context" }

func TestUpdatePartRejectsStaleOnHandBeforeWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	const orgID = int64(42)
	const partID = int64(9)
	expectedOnHand := int32(10)
	requestedOnHand := int32(15)
	params := models.UpdateParams{
		OrgID: orgID, ID: partID, OnHand: &requestedOnHand, ExpectedOnHand: &expectedOnHand,
		Reason: models.AdjustmentReasonReceivedShipment,
	}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().GetForUpdate(gomock.Any(), orgID, partID).Return(
		&models.InventoryPart{ID: partID, OrgID: orgID, OnHand: 8, Allocated: 0}, nil,
	)

	_, err := service.UpdatePart(t.Context(), params)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "refresh")
}

func TestUpdatePartRejectsOnHandBelowAllocatedBeforeWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	const orgID = int64(42)
	const partID = int64(9)
	onHand := int32(1)
	expectedOnHand := int32(5)
	params := models.UpdateParams{
		OrgID: orgID, ID: partID, OnHand: &onHand, ExpectedOnHand: &expectedOnHand,
		Reason: models.AdjustmentReasonCycleCount,
	}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().GetForUpdate(gomock.Any(), orgID, partID).Return(
		&models.InventoryPart{ID: partID, OrgID: orgID, OnHand: 5, Allocated: 2}, nil,
	)

	_, err := service.UpdatePart(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
}
