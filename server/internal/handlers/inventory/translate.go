package inventory

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
)

func toListFilter(req *pb.ListPartsRequest, orgID int64) models.ListFilter {
	out := models.ListFilter{
		OrgID:         orgID,
		SiteIDs:       req.GetSiteIds(),
		Types:         req.GetTypes(),
		LowStockOnly:  req.GetLowStockOnly(),
		Limit:         req.GetLimit(),
	}
	if req.CursorId != nil {
		v := req.GetCursorId()
		out.CursorID = &v
	}
	return out
}

func toCreateParams(req *pb.CreatePartRequest, orgID int64) models.CreateParams {
	out := models.CreateParams{
		OrgID:        orgID,
		Name:         req.GetName(),
		Type:         req.GetType(),
		OnHand:       req.GetOnHand(),
		ReorderPoint: req.GetReorderPoint(),
	}
	if req.Manufacturer != nil {
		v := req.GetManufacturer()
		out.Manufacturer = &v
	}
	if req.PartNumber != nil {
		v := req.GetPartNumber()
		out.PartNumber = &v
	}
	if req.SiteId != nil {
		v := req.GetSiteId()
		out.SiteID = &v
	}
	if req.BinLocation != nil {
		v := req.GetBinLocation()
		out.BinLocation = &v
	}
	return out
}

func toUpdateParams(req *pb.UpdatePartRequest, orgID int64) models.UpdateParams {
	out := models.UpdateParams{
		ID:    req.GetId(),
		OrgID: orgID,
		// defined_only on the proto enum gates malformed values; this
		// is a straight int32 → int16 cast.
		Reason: models.AdjustmentReason(req.GetReason()), //nolint:gosec // enum is bounded by buf.validate defined_only; int32 → int16 cast is safe.
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
	if req.Notes != nil {
		v := req.GetNotes()
		out.Notes = &v
	}
	return out
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
		v := *p.Manufacturer
		out.Manufacturer = &v
	}
	if p.PartNumber != nil {
		v := *p.PartNumber
		out.PartNumber = &v
	}
	if p.SiteID != nil {
		v := *p.SiteID
		out.SiteId = &v
	}
	if p.BinLocation != nil {
		v := *p.BinLocation
		out.BinLocation = &v
	}
	return out
}

func toListPartsResponse(rows []models.InventoryPart) *pb.ListPartsResponse {
	out := make([]*pb.InventoryPart, 0, len(rows))
	for i := range rows {
		out = append(out, toProtoPart(&rows[i]))
	}
	return &pb.ListPartsResponse{Parts: out}
}

func toGetInsightsResponse(insights *models.InventoryInsights) *pb.GetInsightsResponse {
	if insights == nil {
		return &pb.GetInsightsResponse{}
	}
	return &pb.GetInsightsResponse{
		TotalOnHand:    insights.TotalOnHand,
		TotalAllocated: insights.TotalAllocated,
		LowStockCount:  insights.LowStockCount,
		SitesCount:     insights.SitesCount,
	}
}

func toListPartsBySiteResponse(rows []models.InventoryPart) *pb.ListPartsBySiteResponse {
	out := make([]*pb.InventoryPart, 0, len(rows))
	for i := range rows {
		out = append(out, toProtoPart(&rows[i]))
	}
	return &pb.ListPartsBySiteResponse{Parts: out}
}

func toImportCsvPreviewResponse(rows []models.CsvPreviewRow) *pb.ImportCsvPreviewResponse {
	out := make([]*pb.CsvPreviewRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, &pb.CsvPreviewRow{
			RowNumber:    int32(row.RowNumber), //nolint:gosec // row numbers are bounded by maxCsvPreviewRows
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
	}
	return &pb.ImportCsvPreviewResponse{Rows: out}
}

func fromProtoPreviewRows(pbRows []*pb.CsvPreviewRow) []models.CsvPreviewRow {
	out := make([]models.CsvPreviewRow, 0, len(pbRows))
	for _, r := range pbRows {
		out = append(out, models.CsvPreviewRow{
			RowNumber:    int(r.GetRowNumber()),
			Name:         r.GetName(),
			Type:         r.GetType(),
			Manufacturer: r.GetManufacturer(),
			PartNumber:   r.GetPartNumber(),
			SiteName:     r.GetSiteName(),
			OnHand:       r.GetOnHand(),
			ReorderPoint: r.GetReorderPoint(),
			BinLocation:  r.GetBinLocation(),
			Error:        r.GetError(),
		})
	}
	return out
}
