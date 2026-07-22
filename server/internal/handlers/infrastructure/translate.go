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

// toUpdateParams maps the update request. enabled is optional with
// presence tracking: the pointer passes through so an omitted field
// preserves the device's current value atomically in the UPDATE
// statement itself — an unrelated update can't silently disable (or
// re-enable) a device, even racing a concurrent toggle.
func toUpdateParams(req *pb.UpdateInfrastructureDeviceRequest, orgID int64) models.UpdateParams {
	return models.UpdateParams{
		OrgID:        orgID,
		ID:           req.GetId(),
		SiteID:       req.GetSiteId(),
		BuildingName: req.GetBuildingName(),
		RackName:     req.GetRackName(),
		Name:         req.GetName(),
		DeviceKind:   req.GetDeviceKind(),
		FanCount:     req.GetFanCount(),
		Enabled:      req.Enabled,
		DriverType:   req.GetDriverType(),
		DriverConfig: json.RawMessage(req.GetDriverConfig()),
	}
}

// toProtoDevice maps a domain device to the wire shape.
// includeDriverConfig gates the opaque config blob: it carries the OT
// control topology (endpoint, unit ID, register address) and is only
// returned to callers holding site:manage for the device's site —
// site:read callers get the display fields with an empty
// driver_config.
func toProtoDevice(d *models.Device, includeDriverConfig bool) *pb.InfrastructureDevice {
	if d == nil {
		return nil
	}
	out := &pb.InfrastructureDevice{
		Id:           d.ID,
		SiteId:       d.SiteID,
		SiteLabel:    d.SiteLabel,
		BuildingName: d.BuildingName,
		RackName:     d.RackName,
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
	return out
}
