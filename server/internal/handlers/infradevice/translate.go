package infradevice

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/infradevice/v1"
	"github.com/block/proto-fleet/server/internal/domain/infradevice/models"
)

func toListFilter(req *pb.ListInfraDevicesRequest, orgID int64) models.ListFilter {
	out := models.ListFilter{OrgID: orgID}
	if req.GetSiteId() != 0 {
		v := req.GetSiteId()
		out.SiteID = &v
	}
	if req.GetBuildingId() != 0 {
		v := req.GetBuildingId()
		out.BuildingID = &v
	}
	if req.GetDeviceType() != 0 {
		v := int16(req.GetDeviceType()) //nolint:gosec // enum is bounded by buf.validate defined_only
		out.DeviceType = &v
	}
	if req.GetStatus() != 0 {
		v := int16(req.GetStatus()) //nolint:gosec // enum is bounded by buf.validate defined_only
		out.Status = &v
	}
	return out
}

func toCreateParams(req *pb.CreateInfraDeviceRequest, orgID int64) models.CreateParams {
	out := models.CreateParams{
		OrgID:       orgID,
		Name:        req.GetName(),
		DeviceType:  int16(req.GetDeviceType()),  //nolint:gosec // enum is bounded by buf.validate defined_only
		Status:      int16(req.GetStatus()),       //nolint:gosec // enum is bounded by buf.validate defined_only
		ControlMode: int16(req.GetControlMode()),  //nolint:gosec // enum is bounded by buf.validate defined_only
	}
	if req.Subtype != nil {
		v := req.GetSubtype()
		out.Subtype = &v
	}
	if req.SiteId != nil {
		v := req.GetSiteId()
		out.SiteID = &v
	}
	if req.BuildingId != nil {
		v := req.GetBuildingId()
		out.BuildingID = &v
	}
	if req.IpAddress != nil {
		v := req.GetIpAddress()
		out.IPAddress = &v
	}
	if req.Rpm != nil {
		v := req.GetRpm()
		out.RPM = &v
	}
	if req.Protocol != nil {
		v := req.GetProtocol()
		out.Protocol = &v
	}
	return out
}

func toUpdateParams(req *pb.UpdateInfraDeviceRequest, orgID int64) models.UpdateParams {
	out := models.UpdateParams{
		ID:    req.GetId(),
		OrgID: orgID,
	}
	if req.Name != nil {
		v := req.GetName()
		out.Name = &v
	}
	if req.IpAddress != nil {
		v := req.GetIpAddress()
		out.IPAddress = &v
	}
	if req.ControlMode != nil {
		v := int16(req.GetControlMode()) //nolint:gosec // enum is bounded by buf.validate defined_only
		out.ControlMode = &v
	}
	return out
}

func toProtoInfraDevice(d *models.InfraDevice) *pb.InfraDevice {
	if d == nil {
		return nil
	}
	out := &pb.InfraDevice{
		Id:           d.ID,
		Name:         d.Name,
		DeviceType:   pb.DeviceType(d.DeviceType),
		SiteName:     d.SiteName,
		BuildingName: d.BuildingName,
		Status:       pb.DeviceStatus(d.Status),
		ControlMode:  pb.ControlMode(d.ControlMode),
		CreatedAt:    timestamppb.New(d.CreatedAt),
		UpdatedAt:    timestamppb.New(d.UpdatedAt),
	}
	if d.Subtype != nil {
		v := *d.Subtype
		out.Subtype = &v
	}
	if d.SiteID != nil {
		v := *d.SiteID
		out.SiteId = &v
	}
	if d.BuildingID != nil {
		v := *d.BuildingID
		out.BuildingId = &v
	}
	if d.IPAddress != nil {
		v := *d.IPAddress
		out.IpAddress = &v
	}
	if d.RPM != nil {
		v := *d.RPM
		out.Rpm = &v
	}
	if d.Protocol != nil {
		v := *d.Protocol
		out.Protocol = &v
	}
	if d.LastSeen != nil {
		out.LastSeen = timestamppb.New(*d.LastSeen)
	}
	return out
}

func toListInfraDevicesResponse(rows []models.InfraDevice) *pb.ListInfraDevicesResponse {
	out := make([]*pb.InfraDevice, 0, len(rows))
	for i := range rows {
		out = append(out, toProtoInfraDevice(&rows[i]))
	}
	return &pb.ListInfraDevicesResponse{Devices: out}
}

func toScanNetworkResponse(discovered []models.DiscoveredDevice) *pb.ScanNetworkResponse {
	out := make([]*pb.DiscoveredDevice, 0, len(discovered))
	for _, d := range discovered {
		entry := &pb.DiscoveredDevice{
			IpAddress:  d.IPAddress,
			DeviceType: pb.DeviceType(d.DeviceType),
		}
		if d.MACAddress != nil {
			v := *d.MACAddress
			entry.MacAddress = &v
		}
		if d.Hostname != nil {
			v := *d.Hostname
			entry.Hostname = &v
		}
		if d.Protocol != nil {
			v := *d.Protocol
			entry.Protocol = &v
		}
		out = append(out, entry)
	}
	return &pb.ScanNetworkResponse{Devices: out}
}

func toPairEntries(req *pb.PairDevicesRequest) []models.PairEntry {
	out := make([]models.PairEntry, 0, len(req.GetEntries()))
	for _, e := range req.GetEntries() {
		entry := models.PairEntry{
			Name:        e.GetName(),
			DeviceType:  int16(e.GetDeviceType()),  //nolint:gosec // enum is bounded by buf.validate defined_only
			Status:      int16(e.GetStatus()),       //nolint:gosec // enum is bounded by buf.validate defined_only
			ControlMode: int16(e.GetControlMode()),  //nolint:gosec // enum is bounded by buf.validate defined_only
		}
		if e.Subtype != nil {
			v := e.GetSubtype()
			entry.Subtype = &v
		}
		if e.SiteId != nil {
			v := e.GetSiteId()
			entry.SiteID = &v
		}
		if e.BuildingId != nil {
			v := e.GetBuildingId()
			entry.BuildingID = &v
		}
		if e.IpAddress != nil {
			v := e.GetIpAddress()
			entry.IPAddress = &v
		}
		if e.Protocol != nil {
			v := e.GetProtocol()
			entry.Protocol = &v
		}
		out = append(out, entry)
	}
	return out
}
