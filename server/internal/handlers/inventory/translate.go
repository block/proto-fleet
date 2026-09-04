package inventory

import (
	"strconv"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
)

func toListFilter(req *pb.ListInventoryPartsRequest, orgID int64) (models.ListFilter, error) {
	filter := req.GetFilter()
	out := models.ListFilter{
		OrgID:        orgID,
		SiteIDs:      filter.GetSiteIds(),
		Types:        filter.GetTypes(),
		LowStockOnly: filter.GetLowStockOnly(),
		Limit:        req.GetPageSize(),
	}
	if req.GetPageToken() != "" {
		cursorID, err := strconv.ParseInt(req.GetPageToken(), 10, 64)
		if err != nil || cursorID <= 0 {
			return models.ListFilter{}, fleeterror.NewInvalidArgumentError("invalid page_token")
		}
		out.CursorID = &cursorID
	}
	return out, nil
}

func toCreateParams(req *pb.CreateInventoryPartRequest, orgID int64) models.CreateParams {
	out := models.CreateParams{
		OrgID:        orgID,
		Name:         req.GetName(),
		Type:         req.GetType(),
		OnHand:       req.GetOnHand(),
		ReorderPoint: req.GetReorderPoint(),
	}
	if req.GetManufacturer() != "" {
		v := req.GetManufacturer()
		out.Manufacturer = &v
	}
	if req.GetPartNumber() != "" {
		v := req.GetPartNumber()
		out.PartNumber = &v
	}
	if req.SiteId != nil {
		v := req.GetSiteId()
		out.SiteID = &v
	}
	if req.GetBinLocation() != "" {
		v := req.GetBinLocation()
		out.BinLocation = &v
	}
	return out
}

func toUpdateParams(req *pb.UpdateInventoryPartRequest, orgID int64) (models.UpdateParams, error) {
	reason := int32(req.GetReason())
	if reason < 1 || reason > 5 {
		return models.UpdateParams{}, fleeterror.NewInvalidArgumentError("invalid adjustment reason")
	}
	out := models.UpdateParams{
		ID:     req.GetId(),
		OrgID:  orgID,
		Reason: models.AdjustmentReason(reason), //nolint:gosec // explicit range check above proves conversion safety
	}
	if req.OnHand != nil {
		v := req.GetOnHand()
		out.OnHand = &v
	}
	if req.ReorderPoint != nil {
		v := req.GetReorderPoint()
		out.ReorderPoint = &v
	}
	if req.BinLocation != nil {
		v := req.GetBinLocation()
		out.BinLocation = &v
	}
	return out, nil
}

func toProtoPart(p *models.InventoryPart) *pb.InventoryPart {
	if p == nil {
		return nil
	}
	out := &pb.InventoryPart{
		Id:           p.ID,
		Name:         p.Name,
		Type:         p.Type,
		SiteName:     p.SiteName,
		OnHand:       p.OnHand,
		Allocated:    p.Allocated,
		ReorderPoint: p.ReorderPoint,
		CreatedAt:    timestamppb.New(p.CreatedAt),
		UpdatedAt:    timestamppb.New(p.UpdatedAt),
	}
	if p.Manufacturer != nil {
		out.Manufacturer = *p.Manufacturer
	}
	if p.PartNumber != nil {
		out.PartNumber = *p.PartNumber
	}
	if p.SiteID != nil {
		v := *p.SiteID
		out.SiteId = &v
	}
	if p.BinLocation != nil {
		out.BinLocation = *p.BinLocation
	}
	return out
}

func toListPartsResponse(page *models.InventoryPage) *pb.ListInventoryPartsResponse {
	out := make([]*pb.InventoryPart, 0, len(page.Parts))
	for i := range page.Parts {
		out = append(out, toProtoPart(&page.Parts[i]))
	}
	response := &pb.ListInventoryPartsResponse{Parts: out, TotalCount: page.TotalCount}
	if page.NextCursorID != nil {
		response.NextPageToken = strconv.FormatInt(*page.NextCursorID, 10)
	}
	return response
}

func toGetInsightsResponse(insights *models.InventoryInsights) *pb.GetInventoryInsightsResponse {
	if insights == nil {
		return &pb.GetInventoryInsightsResponse{}
	}
	return &pb.GetInventoryInsightsResponse{
		Insights: &pb.InventoryInsights{
			TotalOnHand:    insights.TotalOnHand,
			TotalAllocated: insights.TotalAllocated,
			LowStockCount:  insights.LowStockCount,
			SitesCount:     insights.SitesCount,
			PartTypes:      insights.PartTypes,
		},
	}
}

func toListPartsBySiteResponse(rows []models.InventoryPart) *pb.ListPartsBySiteResponse {
	out := make([]*pb.InventoryPart, 0, len(rows))
	for i := range rows {
		out = append(out, toProtoPart(&rows[i]))
	}
	return &pb.ListPartsBySiteResponse{Parts: out}
}

func toImportCsvPreviewResponse(rows []models.CsvPreviewRow) *pb.ImportInventoryCsvResponse {
	out := make([]*pb.CsvPreviewRow, 0, len(rows))
	var validCount int32
	var errorCount int32
	for _, row := range rows {
		out = append(out, &pb.CsvPreviewRow{
			RowNumber:    int32(row.RowNumber), //nolint:gosec // CSV import is capped at 500 rows
			Name:         row.Name,
			Type:         row.Type,
			Manufacturer: row.Manufacturer,
			PartNumber:   row.PartNumber,
			SiteName:     row.SiteName,
			OnHand:       row.OnHand,
			ReorderPoint: row.ReorderPoint,
			BinLocation:  row.BinLocation,
			Error:        row.Error,
		})
		if row.Error == "" {
			validCount++
		} else {
			errorCount++
		}
	}
	return &pb.ImportInventoryCsvResponse{Rows: out, ValidCount: validCount, ErrorCount: errorCount}
}
