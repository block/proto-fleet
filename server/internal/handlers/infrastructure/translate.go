package infrastructure

import (
	"encoding/json"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
)

func toListFilter(req *pb.ListInfrastructureDevicesRequest, orgID int64) models.ListFilter {
	return models.ListFilter{
		OrgID:   orgID,
		SiteIDs: req.GetSiteIds(),
	}
}

func toCreateParams(req *pb.CreateInfrastructureDeviceRequest, orgID int64) models.CreateParams {
	// enabled is optional with presence tracking: an omitted field
	// defaults to true (matching the column default), so API-created
	// devices are enabled unless the client explicitly disables them.
	enabled := true
	if req.Enabled != nil {
		enabled = req.GetEnabled()
	}
	return models.CreateParams{
		OrgID:        orgID,
		SiteID:       req.GetSiteId(),
		BuildingName: req.GetBuildingName(),
		RackName:     req.GetRackName(),
		Name:         req.GetName(),
		DeviceKind:   req.GetDeviceKind(),
		FanCount:     req.GetFanCount(),
		Enabled:      enabled,
		DriverType:   req.GetDriverType(),
		DriverConfig: json.RawMessage(req.GetDriverConfig()),
	}
}

// toUpdateParams maps the update request. Enabled and rack_name use
// presence tracking: their pointers pass through so omitted fields are
// preserved atomically in the UPDATE statement instead of writing back
// stale or proto-default values.
func toUpdateParams(req *pb.UpdateInfrastructureDeviceRequest, orgID int64) models.UpdateParams {
	return models.UpdateParams{
		OrgID:        orgID,
		ID:           req.GetId(),
		SiteID:       req.GetSiteId(),
		BuildingName: req.GetBuildingName(),
		RackName:     req.RackName,
		Name:         req.GetName(),
		DeviceKind:   req.GetDeviceKind(),
		FanCount:     req.GetFanCount(),
		Enabled:      req.Enabled,
		DriverType:   req.GetDriverType(),
		DriverConfig: json.RawMessage(req.GetDriverConfig()),
	}
}

// toProtoDevice maps a domain device to the wire shape.
// includeDriverConfig gates the opaque OT control topology, while
// includeRackName gates physical rack inventory. Site-readable callers
// receive the remaining display fields when either permission is absent.
func toProtoDevice(d *models.Device, includeDriverConfig, includeRackName bool) *pb.InfrastructureDevice {
	if d == nil {
		return nil
	}
	out := &pb.InfrastructureDevice{
		Id:           d.ID,
		SiteId:       d.SiteID,
		SiteLabel:    d.SiteLabel,
		BuildingName: d.BuildingName,
		Name:         d.Name,
		DeviceKind:   d.DeviceKind,
		FanCount:     d.FanCount,
		Enabled:      d.Enabled,
		DriverType:   d.DriverType,
		CreatedAt:    timestamppb.New(d.CreatedAt),
		UpdatedAt:    timestamppb.New(d.UpdatedAt),
	}
	if includeDriverConfig {
		out.DriverConfig = string(d.DriverConfig)
	}
	if includeRackName {
		out.RackName = d.RackName
	}
	return out
}
