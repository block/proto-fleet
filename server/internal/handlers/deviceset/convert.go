package deviceset

import (
	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
)

// --- DeviceSetType <-> CollectionType ---

func toCollectionType(t dspb.DeviceSetType) collectionpb.CollectionType {
	switch t {
	case dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP:
		return collectionpb.CollectionType_COLLECTION_TYPE_GROUP
	case dspb.DeviceSetType_DEVICE_SET_TYPE_RACK:
		return collectionpb.CollectionType_COLLECTION_TYPE_RACK
	case dspb.DeviceSetType_DEVICE_SET_TYPE_UNSPECIFIED:
		return collectionpb.CollectionType_COLLECTION_TYPE_UNSPECIFIED
	default:
		return collectionpb.CollectionType_COLLECTION_TYPE_UNSPECIFIED
	}
}

func toDeviceSetType(t collectionpb.CollectionType) dspb.DeviceSetType {
	switch t {
	case collectionpb.CollectionType_COLLECTION_TYPE_GROUP:
		return dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP
	case collectionpb.CollectionType_COLLECTION_TYPE_RACK:
		return dspb.DeviceSetType_DEVICE_SET_TYPE_RACK
	case collectionpb.CollectionType_COLLECTION_TYPE_UNSPECIFIED:
		return dspb.DeviceSetType_DEVICE_SET_TYPE_UNSPECIFIED
	default:
		return dspb.DeviceSetType_DEVICE_SET_TYPE_UNSPECIFIED
	}
}

// --- DeviceSet <-> DeviceCollection ---

func toDeviceSet(c *collectionpb.DeviceCollection) *dspb.DeviceSet {
	if c == nil {
		return nil
	}
	ds := &dspb.DeviceSet{
		Id:          c.Id,
		Type:        toDeviceSetType(c.Type),
		Label:       c.Label,
		Description: c.Description,
		DeviceCount: c.DeviceCount,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
	switch td := c.TypeDetails.(type) {
	case *collectionpb.DeviceCollection_RackInfo:
		ds.TypeDetails = &dspb.DeviceSet_RackInfo{RackInfo: toDeviceSetRackInfo(td.RackInfo)}
	case *collectionpb.DeviceCollection_GroupInfo:
		ds.TypeDetails = &dspb.DeviceSet_GroupInfo{GroupInfo: &dspb.GroupInfo{}}
	}
	return ds
}

// --- RackInfo ---

func toDeviceSetRackInfo(ri *collectionpb.RackInfo) *dspb.RackInfo {
	if ri == nil {
		return nil
	}
	return &dspb.RackInfo{
		Rows:        ri.Rows,
		Columns:     ri.Columns,
		Zone:        ri.Zone,
		OrderIndex:  dspb.RackOrderIndex(ri.OrderIndex),
		CoolingType: dspb.RackCoolingType(ri.CoolingType),
	}
}

func toCollectionRackInfo(ri *dspb.RackInfo) *collectionpb.RackInfo {
	if ri == nil {
		return nil
	}
	return &collectionpb.RackInfo{
		Rows:        ri.Rows,
		Columns:     ri.Columns,
		Zone:        ri.Zone,
		OrderIndex:  collectionpb.RackOrderIndex(ri.OrderIndex),
		CoolingType: collectionpb.RackCoolingType(ri.CoolingType),
	}
}

// --- RackSlot ---

func toDeviceSetRackSlot(s *collectionpb.RackSlot) *dspb.RackSlot {
	if s == nil {
		return nil
	}
	return &dspb.RackSlot{
		DeviceIdentifier: s.DeviceIdentifier,
		Position:         toDeviceSetRackSlotPosition(s.Position),
	}
}

func toCollectionRackSlot(s *dspb.RackSlot) *collectionpb.RackSlot {
	if s == nil {
		return nil
	}
	return &collectionpb.RackSlot{
		DeviceIdentifier: s.DeviceIdentifier,
		Position:         toCollectionRackSlotPosition(s.Position),
	}
}

// --- RackSlotPosition ---

func toDeviceSetRackSlotPosition(p *collectionpb.RackSlotPosition) *dspb.RackSlotPosition {
	if p == nil {
		return nil
	}
	return &dspb.RackSlotPosition{Row: p.Row, Column: p.Column}
}

func toCollectionRackSlotPosition(p *dspb.RackSlotPosition) *collectionpb.RackSlotPosition {
	if p == nil {
		return nil
	}
	return &collectionpb.RackSlotPosition{Row: p.Row, Column: p.Column}
}

// --- DeviceSetMember <-> CollectionMember ---

func toDeviceSetMember(m *collectionpb.CollectionMember) *dspb.DeviceSetMember {
	if m == nil {
		return nil
	}
	dsm := &dspb.DeviceSetMember{
		DeviceIdentifier: m.DeviceIdentifier,
		AddedAt:          m.AddedAt,
	}
	if rack := m.GetRack(); rack != nil {
		dsm.MemberDetails = &dspb.DeviceSetMember_Rack{
			Rack: &dspb.RackMemberDetails{
				SlotPosition: toDeviceSetRackSlotPosition(rack.SlotPosition),
			},
		}
	}
	return dsm
}

// --- DeviceSetStats <-> CollectionStats ---

func toDeviceSetStats(s *collectionpb.CollectionStats) *dspb.DeviceSetStats {
	if s == nil {
		return nil
	}
	stats := &dspb.DeviceSetStats{
		DeviceSetId:               s.CollectionId,
		DeviceCount:               s.DeviceCount,
		ReportingCount:            s.ReportingCount,
		TotalHashrateThs:          s.TotalHashrateThs,
		AvgEfficiencyJth:          s.AvgEfficiencyJth,
		TotalPowerKw:              s.TotalPowerKw,
		MinTemperatureC:           s.MinTemperatureC,
		MaxTemperatureC:           s.MaxTemperatureC,
		HashingCount:              s.HashingCount,
		BrokenCount:               s.BrokenCount,
		OfflineCount:              s.OfflineCount,
		SleepingCount:             s.SleepingCount,
		HashrateReportingCount:    s.HashrateReportingCount,
		EfficiencyReportingCount:  s.EfficiencyReportingCount,
		PowerReportingCount:       s.PowerReportingCount,
		TemperatureReportingCount: s.TemperatureReportingCount,
		ControlBoardIssueCount:    s.ControlBoardIssueCount,
		FanIssueCount:             s.FanIssueCount,
		HashBoardIssueCount:       s.HashBoardIssueCount,
		PsuIssueCount:             s.PsuIssueCount,
	}
	for _, ss := range s.SlotStatuses {
		stats.SlotStatuses = append(stats.SlotStatuses, &dspb.RackSlotStatus{
			Row:    ss.Row,
			Column: ss.Column,
			Status: dspb.SlotDeviceStatus(ss.Status),
		})
	}
	return stats
}

// --- Request Converters ---

func toCollectionCreateReq(r *dspb.CreateDeviceSetRequest) *collectionpb.CreateCollectionRequest {
	req := &collectionpb.CreateCollectionRequest{
		Type:           toCollectionType(r.Type),
		Label:          r.Label,
		Description:    r.Description,
		DeviceSelector: r.DeviceSelector,
	}
	switch td := r.TypeDetails.(type) {
	case *dspb.CreateDeviceSetRequest_RackInfo:
		req.TypeDetails = &collectionpb.CreateCollectionRequest_RackInfo{RackInfo: toCollectionRackInfo(td.RackInfo)}
	case *dspb.CreateDeviceSetRequest_GroupInfo:
		req.TypeDetails = &collectionpb.CreateCollectionRequest_GroupInfo{GroupInfo: &collectionpb.GroupInfo{}}
	}
	return req
}

func toCollectionUpdateReq(r *dspb.UpdateDeviceSetRequest) *collectionpb.UpdateCollectionRequest {
	req := &collectionpb.UpdateCollectionRequest{
		CollectionId:   r.DeviceSetId,
		Label:          r.Label,
		Description:    r.Description,
		DeviceSelector: r.DeviceSelector,
	}
	switch td := r.TypeDetails.(type) {
	case *dspb.UpdateDeviceSetRequest_RackInfo:
		req.TypeDetails = &collectionpb.UpdateCollectionRequest_RackInfo{RackInfo: toCollectionRackInfo(td.RackInfo)}
	case *dspb.UpdateDeviceSetRequest_GroupInfo:
		req.TypeDetails = &collectionpb.UpdateCollectionRequest_GroupInfo{GroupInfo: &collectionpb.GroupInfo{}}
	}
	return req
}

func toCollectionListReq(r *dspb.ListDeviceSetsRequest) *collectionpb.ListCollectionsRequest {
	req := &collectionpb.ListCollectionsRequest{
		Type:                toCollectionType(r.Type),
		PageSize:            r.PageSize,
		PageToken:           r.PageToken,
		Sort:                r.Sort,
		ErrorComponentTypes: r.ErrorComponentTypes,
		Zones:               r.Zones,
	}
	return req
}

func toCollectionSaveRackReq(r *dspb.SaveRackRequest) *collectionpb.SaveRackRequest {
	req := &collectionpb.SaveRackRequest{
		CollectionId:   r.DeviceSetId,
		Label:          r.Label,
		RackInfo:       toCollectionRackInfo(r.RackInfo),
		DeviceSelector: r.DeviceSelector,
	}
	for _, sa := range r.SlotAssignments {
		req.SlotAssignments = append(req.SlotAssignments, toCollectionRackSlot(sa))
	}
	return req
}
