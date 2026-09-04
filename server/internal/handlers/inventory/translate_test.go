package inventory

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToUpdateParamsRangeChecksEnumAndPreservesPresence(t *testing.T) {
	onHand := int32(0)
	params, err := toUpdateParams(&pb.UpdateInventoryPartRequest{Id: 7, OnHand: &onHand, Reason: pb.AdjustmentReason_ADJUSTMENT_REASON_CYCLE_COUNT}, 42)
	require.NoError(t, err)
	require.NotNil(t, params.OnHand)
	assert.Zero(t, *params.OnHand)
	assert.Equal(t, models.AdjustmentReasonCycleCount, params.Reason)

	_, err = toUpdateParams(&pb.UpdateInventoryPartRequest{Reason: pb.AdjustmentReason(99)}, 42)
	assert.Error(t, err)
}

func TestImportPreviewIncludesEveryCsvColumn(t *testing.T) {
	response := toImportCsvPreviewResponse([]models.CsvPreviewRow{{RowNumber: 3, Name: "Fan", Type: "cooling", Manufacturer: "Proto", PartNumber: "F-1", SiteName: "Mine", OnHand: 2, ReorderPoint: 1, BinLocation: "A1"}})
	require.Len(t, response.Rows, 1)
	row := response.Rows[0]
	assert.Equal(t, int32(3), row.RowNumber)
	assert.Equal(t, "Proto", row.Manufacturer)
	assert.Equal(t, "F-1", row.PartNumber)
}
