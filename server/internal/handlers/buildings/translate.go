package buildings

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
)

func toListFilter(req *pb.ListBuildingsRequest, orgID int64) models.ListFilter {
	return models.ListFilter{
		OrgID:             orgID,
		SiteIDs:           req.GetSiteIds(),
		IncludeUnassigned: req.GetIncludeUnassigned(),
	}
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
		out.Placement = &commonpb.PlacementRefs{
			Site: &commonpb.ResourceRef{
				Id:    v,
				Label: b.SiteLabel,
			},
		}
	}
	return out
}

func toListBuildingsResponse(rows []models.BuildingWithCounts) *pb.ListBuildingsResponse {
	out := make([]*pb.BuildingWithCounts, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, &pb.BuildingWithCounts{
			Building:    toProtoBuilding(&row.Building),
			RackCount:   row.RackCount,
			DeviceCount: row.DeviceCount,
			ListStats:   toProtoFleetListStats(row.ListStats),
		})
	}
	return &pb.ListBuildingsResponse{Buildings: out}
}

func toProtoFleetListStats(stats *models.FleetListStats) *commonpb.FleetListStats {
	if stats == nil {
		return nil
	}
	return &commonpb.FleetListStats{
		BuildingCount:             stats.BuildingCount,
		RackCount:                 stats.RackCount,
		DeviceCount:               stats.DeviceCount,
		ReportingCount:            stats.ReportingCount,
		HashrateReportingCount:    stats.HashrateReportingCount,
		EfficiencyReportingCount:  stats.EfficiencyReportingCount,
		PowerReportingCount:       stats.PowerReportingCount,
		TemperatureReportingCount: stats.TemperatureReportingCount,
		TotalHashrateThs:          stats.TotalHashrateThs,
		AvgEfficiencyJth:          stats.AvgEfficiencyJth,
		TotalPowerKw:              stats.TotalPowerKw,
		MinTemperatureC:           stats.MinTemperatureC,
		MaxTemperatureC:           stats.MaxTemperatureC,
		HashingCount:              stats.HashingCount,
		BrokenCount:               stats.BrokenCount,
		OfflineCount:              stats.OfflineCount,
		SleepingCount:             stats.SleepingCount,
		ControlBoardIssueCount:    stats.ControlBoardIssueCount,
		FanIssueCount:             stats.FanIssueCount,
		HashBoardIssueCount:       stats.HashBoardIssueCount,
		PsuIssueCount:             stats.PsuIssueCount,
	}
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

func toAssignDevicesToBuildingParams(req *pb.AssignDevicesToBuildingRequest, orgID int64) models.AssignDevicesToBuildingParams {
	var targetBuildingID *int64
	if req.TargetBuildingId != nil {
		v := req.GetTargetBuildingId()
		targetBuildingID = &v
	}
	return models.AssignDevicesToBuildingParams{
		OrgID:                               orgID,
		TargetBuildingID:                    targetBuildingID,
		DeviceIdentifiers:                   req.GetDeviceIdentifiers(),
		ForceClearConflictingRackMembership: req.GetForceClearConflictingRackMembership(),
	}
}

func toProtoBuildingConflicts(conflicts []models.PerDeviceBuildingConflict) []*pb.PerDeviceBuildingConflict {
	if len(conflicts) == 0 {
		return nil
	}
	out := make([]*pb.PerDeviceBuildingConflict, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, &pb.PerDeviceBuildingConflict{
			DeviceIdentifier:      c.DeviceIdentifier,
			Reason:                toProtoBuildingConflictReason(c.Reason),
			ConflictingBuildingId: c.ConflictingBuildingID,
		})
	}
	return out
}

func toProtoBuildingConflictReason(r models.PerDeviceBuildingConflictReason) pb.PerDeviceBuildingConflictReason {
	switch r {
	case models.ReasonBuildingUnspecified:
		return pb.PerDeviceBuildingConflictReason_PER_DEVICE_BUILDING_CONFLICT_REASON_UNSPECIFIED
	case models.ReasonBuildingDeviceNotFound:
		return pb.PerDeviceBuildingConflictReason_PER_DEVICE_BUILDING_CONFLICT_REASON_DEVICE_NOT_FOUND
	case models.ReasonBuildingDeviceInRackAtOtherBuilding:
		return pb.PerDeviceBuildingConflictReason_PER_DEVICE_BUILDING_CONFLICT_REASON_DEVICE_IN_RACK_AT_OTHER_BUILDING
	case models.ReasonBuildingDeviceInRackAtOtherSite:
		return pb.PerDeviceBuildingConflictReason_PER_DEVICE_BUILDING_CONFLICT_REASON_DEVICE_IN_RACK_AT_OTHER_SITE
	default:
		return pb.PerDeviceBuildingConflictReason_PER_DEVICE_BUILDING_CONFLICT_REASON_UNSPECIFIED
	}
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
