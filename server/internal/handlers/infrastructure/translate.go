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
		Name:         req.GetName(),
		DeviceKind:   req.GetDeviceKind(),
		FanCount:     req.GetFanCount(),
		Enabled:      enabled,
		DriverType:   req.GetDriverType(),
		DriverConfig: json.RawMessage(req.GetDriverConfig()),
	}
}

func toUpdateParams(req *pb.UpdateInfrastructureDeviceRequest, orgID int64) models.UpdateParams {
	return models.UpdateParams{
		OrgID:        orgID,
		ID:           req.GetId(),
		SiteID:       req.GetSiteId(),
		BuildingName: req.GetBuildingName(),
		Name:         req.GetName(),
		DeviceKind:   req.GetDeviceKind(),
		FanCount:     req.GetFanCount(),
		Enabled:      req.GetEnabled(),
		DriverType:   req.GetDriverType(),
		DriverConfig: json.RawMessage(req.GetDriverConfig()),
	}
}

func toProtoDevice(d *models.Device) *pb.InfrastructureDevice {
	if d == nil {
		return nil
	}
	return &pb.InfrastructureDevice{
		Id:           d.ID,
		SiteId:       d.SiteID,
		SiteLabel:    d.SiteLabel,
		BuildingName: d.BuildingName,
		Name:         d.Name,
		DeviceKind:   d.DeviceKind,
		FanCount:     d.FanCount,
		Enabled:      d.Enabled,
		DriverType:   d.DriverType,
		DriverConfig: string(d.DriverConfig),
		CreatedAt:    timestamppb.New(d.CreatedAt),
		UpdatedAt:    timestamppb.New(d.UpdatedAt),
	}
}

func toListResponse(devices []models.Device) *pb.ListInfrastructureDevicesResponse {
	out := make([]*pb.InfrastructureDevice, 0, len(devices))
	for i := range devices {
		out = append(out, toProtoDevice(&devices[i]))
	}
	return &pb.ListInfrastructureDevicesResponse{Devices: out}
}
