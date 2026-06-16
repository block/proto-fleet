package buildings

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
)

func toListFilter(req *pb.ListBuildingsRequest, orgID int64) models.ListFilter {
	out := models.ListFilter{OrgID: orgID}
	switch f := req.GetSiteFilter().(type) {
	case *pb.ListBuildingsRequest_SiteId:
		v := f.SiteId
		out.SiteID = &v
	case *pb.ListBuildingsRequest_UnassignedOnly:
		out.UnassignedOnly = f.UnassignedOnly
	}
	return out
}

func toCreateParams(req *pb.CreateBuildingRequest, orgID int64) models.CreateParams {
	var siteID *int64
	if req.SiteId != nil {
		v := req.GetSiteId()
		siteID = &v
	}
	// defined_only on the proto enum gates malformed values; this is a
	// straight int32 → int16 cast.
	return models.CreateParams{
		OrgID:                 orgID,
		SiteID:                siteID,
		Name:                  req.GetName(),
		Description:           req.GetDescription(),
		PowerKw:               req.GetPowerKw(),
		OverheadKw:            req.GetOverheadKw(),
		Aisles:                req.GetAisles(),
		PhysicalRackCount:     req.GetPhysicalRackCount(),
		RacksPerAisle:         req.GetRacksPerAisle(),
		DefaultRackRows:       req.GetDefaultRackRows(),
		DefaultRackColumns:    req.GetDefaultRackColumns(),
		DefaultRackOrderIndex: models.RackOrderIndex(req.GetDefaultRackOrderIndex()), //nolint:gosec // enum is bounded by buf.validate defined_only; int32 → int16 cast is safe.
	}
}

func toUpdateParams(req *pb.UpdateBuildingRequest, orgID int64) models.UpdateParams {
	// defined_only on the proto enum gates malformed values; this is a
	// straight int32 → int16 cast.
	return models.UpdateParams{
		OrgID:                 orgID,
		ID:                    req.GetId(),
		Name:                  req.GetName(),
		Description:           req.GetDescription(),
		PowerKw:               req.GetPowerKw(),
		OverheadKw:            req.GetOverheadKw(),
		Aisles:                req.GetAisles(),
		PhysicalRackCount:     req.GetPhysicalRackCount(),
		RacksPerAisle:         req.GetRacksPerAisle(),
		DefaultRackRows:       req.GetDefaultRackRows(),
		DefaultRackColumns:    req.GetDefaultRackColumns(),
		DefaultRackOrderIndex: models.RackOrderIndex(req.GetDefaultRackOrderIndex()), //nolint:gosec // enum is bounded by buf.validate defined_only; int32 → int16 cast is safe.
	}
}

func toProtoBuilding(b *models.Building) *pb.Building {
	if b == nil {
		return nil
	}
	out := &pb.Building{
		Id:                    b.ID,
		Name:                  b.Name,
		Description:           b.Description,
		PowerKw:               b.PowerKw,
		OverheadKw:            b.OverheadKw,
		Aisles:                b.Aisles,
		PhysicalRackCount:     b.PhysicalRackCount,
		RacksPerAisle:         b.RacksPerAisle,
		DefaultRackRows:       b.DefaultRackRows,
		DefaultRackColumns:    b.DefaultRackColumns,
		DefaultRackOrderIndex: pb.RackOrderIndex(b.DefaultRackOrderIndex),
		CreatedAt:             timestamppb.New(b.CreatedAt),
		UpdatedAt:             timestamppb.New(b.UpdatedAt),
	}
	if b.SiteID != nil {
		v := *b.SiteID
		out.SiteId = &v
	}
	return out
}

func toListBuildingsResponse(rows []models.BuildingWithCounts) *pb.ListBuildingsResponse {
	out := make([]*pb.BuildingWithCounts, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, &pb.BuildingWithCounts{
			Building:  toProtoBuilding(&row.Building),
			RackCount: row.RackCount,
		})
	}
	return &pb.ListBuildingsResponse{Buildings: out}
}

func toListBuildingRacksResponse(rows []models.BuildingRack, nextPageToken string) *pb.ListBuildingRacksResponse {
	out := make([]*pb.BuildingRack, 0, len(rows))
	for i := range rows {
		row := rows[i]
		entry := &pb.BuildingRack{
			RackId:    row.RackID,
			RackLabel: row.RackLabel,
		}
		if row.AisleIndex != nil {
			v := *row.AisleIndex
			entry.AisleIndex = &v
		}
		if row.PositionInAisle != nil {
			v := *row.PositionInAisle
			entry.PositionInAisle = &v
		}
		out = append(out, entry)
	}
	return &pb.ListBuildingRacksResponse{Racks: out, NextPageToken: nextPageToken}
}

func toAssignRacksToBuildingParams(req *pb.AssignRacksToBuildingRequest, orgID int64) models.AssignRacksToBuildingParams {
	out := models.AssignRacksToBuildingParams{
		OrgID: orgID,
		Racks: make([]models.RackPlacementParam, 0, len(req.GetRacks())),
	}
	if req.TargetBuildingId != nil {
		v := req.GetTargetBuildingId()
		out.TargetBuildingID = &v
	}
	for _, rp := range req.GetRacks() {
		entry := models.RackPlacementParam{RackID: rp.GetRackId()}
		if rp.AisleIndex != nil {
			v := rp.GetAisleIndex()
			entry.AisleIndex = &v
		}
		if rp.PositionInAisle != nil {
			v := rp.GetPositionInAisle()
			entry.PositionInAisle = &v
		}
		out.Racks = append(out.Racks, entry)
	}
	return out
}
