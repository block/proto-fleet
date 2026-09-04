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

func TestInventoryParseCsvPreviewResolvesSitesAndReportsErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	service := NewService(store, nil, nil)
	const orgID = int64(42)

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

func TestConfirmCsvImportRejectsAnyInvalidRowWithoutWrites(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	const orgID = int64(42)

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
	service := NewService(store, transactor, nil)
	const orgID = int64(42)
	const siteID = int64(7)

	store.EXPECT().ResolveSiteByName(gomock.Any(), orgID, "Repair Depot").Return(siteID, nil)
	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
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
	service := NewService(mocks.NewMockInventoryStore(ctrl), nil, nil)

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
	reorderPoint := int32(3)
	params := models.UpdateParams{
		OrgID: orgID, ID: partID, OnHand: &onHand, ReorderPoint: &reorderPoint,
		Reason: models.AdjustmentReasonCycleCount,
	}
	oldPart := &models.InventoryPart{ID: partID, OrgID: orgID, Name: "Fan", OnHand: 10, Allocated: 2, ReorderPoint: 1}
	newPart := &models.InventoryPart{ID: partID, OrgID: orgID, Name: "Fan", OnHand: 8, Allocated: 2, ReorderPoint: 3}
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

func TestUpdatePartRejectsOnHandBelowAllocatedBeforeWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInventoryStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	service := NewService(store, transactor, nil)
	const orgID = int64(42)
	const partID = int64(9)
	onHand := int32(1)
	params := models.UpdateParams{OrgID: orgID, ID: partID, OnHand: &onHand, Reason: models.AdjustmentReasonCycleCount}

	transactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, action func(context.Context) error) error { return action(ctx) },
	)
	store.EXPECT().GetForUpdate(gomock.Any(), orgID, partID).Return(
		&models.InventoryPart{ID: partID, OrgID: orgID, OnHand: 5, Allocated: 2}, nil,
	)

	_, err := service.UpdatePart(t.Context(), params)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
}
