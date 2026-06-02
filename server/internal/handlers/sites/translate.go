package sites

import (
	"google.golang.org/protobuf/types/known/timestamppb"

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

func toReassignParams(req *pb.ReassignDevicesToSiteRequest, orgID int64) models.ReassignDevicesToSiteParams {
	var targetSiteID *int64
	if req.TargetSiteId != nil {
		v := req.GetTargetSiteId()
		targetSiteID = &v
	}
	return models.ReassignDevicesToSiteParams{
		OrgID:             orgID,
		TargetSiteID:      targetSiteID,
		DeviceIdentifiers: req.GetDeviceIdentifiers(),
	}
}

func toAssignBuildingParams(req *pb.AssignBuildingToSiteRequest, orgID int64) models.AssignBuildingToSiteParams {
	var targetSiteID *int64
	if req.TargetSiteId != nil {
		v := req.GetTargetSiteId()
		targetSiteID = &v
	}
	return models.AssignBuildingToSiteParams{
		OrgID:        orgID,
		BuildingID:   req.GetBuildingId(),
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
			Site:          toProtoSite(&row.Site),
			DeviceCount:   row.DeviceCount,
			BuildingCount: row.BuildingCount,
			RackCount:     row.RackCount,
		})
	}
	return &pb.ListSitesResponse{Sites: out}
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
