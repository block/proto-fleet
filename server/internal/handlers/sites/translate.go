package sites

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
)

func toCreateSiteParams(req *pb.CreateSiteRequest, orgID int64) models.CreateSiteParams {
	return models.CreateSiteParams{
		OrgID:           orgID,
		Name:            req.GetName(),
		LocationCity:    req.GetLocationCity(),
		LocationState:   req.GetLocationState(),
		Timezone:        req.GetTimezone(),
		PowerCapacityMw: req.GetPowerCapacityMw(),
		NetworkConfig:   req.GetNetworkConfig(),
		Address:         req.GetAddress(),
		PostalCode:      req.GetPostalCode(),
		Country:         req.GetCountry(),
		Notes:           req.GetNotes(),
	}
}

func toUpdateSiteParams(req *pb.UpdateSiteRequest, orgID int64) models.UpdateSiteParams {
	return models.UpdateSiteParams{
		OrgID:           orgID,
		ID:              req.GetId(),
		Name:            req.GetName(),
		LocationCity:    req.GetLocationCity(),
		LocationState:   req.GetLocationState(),
		Timezone:        req.GetTimezone(),
		PowerCapacityMw: req.GetPowerCapacityMw(),
		NetworkConfig:   req.GetNetworkConfig(),
		Address:         req.GetAddress(),
		PostalCode:      req.GetPostalCode(),
		Country:         req.GetCountry(),
		Notes:           req.GetNotes(),
	}
}

func toAssignDevicesParams(req *pb.AssignDevicesToSiteRequest, orgID int64) models.AssignDevicesToSiteParams {
	var targetSiteID *int64
	if req.TargetSiteId != nil {
		v := req.GetTargetSiteId()
		targetSiteID = &v
	}
	return models.AssignDevicesToSiteParams{
		OrgID:                               orgID,
		TargetSiteID:                        targetSiteID,
		DeviceIdentifiers:                   req.GetDeviceIdentifiers(),
		ForceClearConflictingRackMembership: req.GetForceClearConflictingRackMembership(),
	}
}

func toAssignBuildingsParams(req *pb.AssignBuildingsToSiteRequest, orgID int64) models.AssignBuildingsToSiteParams {
	var targetSiteID *int64
	if req.TargetSiteId != nil {
		v := req.GetTargetSiteId()
		targetSiteID = &v
	}
	return models.AssignBuildingsToSiteParams{
		OrgID:        orgID,
		BuildingIDs:  req.GetBuildingIds(),
		TargetSiteID: targetSiteID,
	}
}

func toAssignRacksToSiteParams(req *pb.AssignRacksToSiteRequest, orgID int64) models.AssignRacksToSiteParams {
	var targetSiteID *int64
	if req.TargetSiteId != nil {
		v := req.GetTargetSiteId()
		targetSiteID = &v
	}
	return models.AssignRacksToSiteParams{
		OrgID:        orgID,
		RackIDs:      req.GetRackIds(),
		TargetSiteID: targetSiteID,
	}
}

// resolveTimezone returns the operator-stored timezone when set,
// falling back to (country, location_state) inference. The override
// lets operators correct sub-state edge cases (N Idaho, El Paso TX,
// W Kentucky) without us maintaining a county-level table.
func resolveTimezone(site *models.Site) string {
	if site.Timezone != "" {
		return site.Timezone
	}
	return sites.InferTimezone(site.Country, site.LocationState)
}

func toProtoSite(site *models.Site) *pb.Site {
	if site == nil {
		return nil
	}
	return &pb.Site{
		Id:              site.ID,
		Name:            site.Name,
		Slug:            site.Slug,
		LocationCity:    site.LocationCity,
		LocationState:   site.LocationState,
		Timezone:        resolveTimezone(site),
		PowerCapacityMw: site.PowerCapacityMw,
		NetworkConfig:   site.NetworkConfig,
		Address:         site.Address,
		PostalCode:      site.PostalCode,
		Country:         site.Country,
		Notes:           site.Notes,
		CreatedAt:       timestamppb.New(site.CreatedAt),
		UpdatedAt:       timestamppb.New(site.UpdatedAt),
	}
}

func toListSitesResponse(rows []models.SiteWithCounts) *pb.ListSitesResponse {
	out := make([]*pb.SiteWithCounts, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, &pb.SiteWithCounts{
			Site:                      toProtoSite(&row.Site),
			DeviceCount:               row.DeviceCount,
			BuildingCount:             row.BuildingCount,
			RackCount:                 row.RackCount,
			InfrastructureDeviceCount: row.InfrastructureDeviceCount,
			ListStats:                 toProtoFleetListStats(row.ListStats),
		})
	}
	return &pb.ListSitesResponse{Sites: out}
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

func toProtoConflicts(conflicts []models.PerDeviceConflict) []*pb.PerDeviceConflict {
	if len(conflicts) == 0 {
		return nil
	}
	out := make([]*pb.PerDeviceConflict, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, &pb.PerDeviceConflict{
			DeviceIdentifier:  c.DeviceIdentifier,
			Reason:            toProtoConflictReason(c.Reason),
			ConflictingSiteId: c.ConflictingSiteID,
		})
	}
	return out
}

func toProtoConflictReason(r models.PerDeviceConflictReason) pb.PerDeviceConflictReason {
	switch r {
	case models.ReasonUnspecified:
		return pb.PerDeviceConflictReason_PER_DEVICE_CONFLICT_REASON_UNSPECIFIED
	case models.ReasonDeviceNotFound:
		return pb.PerDeviceConflictReason_PER_DEVICE_CONFLICT_REASON_DEVICE_NOT_FOUND
	case models.ReasonDeviceInRackAtOtherSite:
		return pb.PerDeviceConflictReason_PER_DEVICE_CONFLICT_REASON_DEVICE_IN_RACK_AT_OTHER_SITE
	default:
		return pb.PerDeviceConflictReason_PER_DEVICE_CONFLICT_REASON_UNSPECIFIED
	}
}
