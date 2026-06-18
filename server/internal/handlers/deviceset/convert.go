package deviceset

import (
	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
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
		Placement:   c.Placement,
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
		SiteId:      ri.SiteId,
		BuildingId:  ri.BuildingId,
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
		SiteId:      ri.SiteId,
		BuildingId:  ri.BuildingId,
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

// maxDeviceSetFilterValues caps the size of free-form repeated filter
// arrays (building_ids, zone_keys, error_component_types). Mirrors the
// fleetmanagement.parseFilter cap. Hardcoded here because the
// constant lives in another package; the value is asserted to match
// in the convert tests.
const maxDeviceSetFilterValues = 1024

// toListCollectionsParams translates a device_set.v1 list request into
// the domain-shaped params consumed by collection.Service.
// ListCollectionsDomain. Threads the new building_ids /
// include_no_building / zone_keys fields, which the deprecated
// collection.v1 proto cannot carry. Caps each repeated filter array
// at maxDeviceSetFilterValues to match the miner-list path.
func toListCollectionsParams(r *dspb.ListDeviceSetsRequest) (collection.ListCollectionsParams, error) {
	if len(r.SiteIds) > maxDeviceSetFilterValues {
		return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
			"site_ids exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	if len(r.BuildingIds) > maxDeviceSetFilterValues {
		return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
			"building_ids exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	if len(r.SiteIds) > maxDeviceSetFilterValues {
		return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
			"site_ids exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	if len(r.ZoneKeys) > maxDeviceSetFilterValues {
		return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
			"zone_keys exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	if len(r.ErrorComponentTypes) > maxDeviceSetFilterValues {
		return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
			"error_component_types exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	if len(r.Zones) > maxDeviceSetFilterValues { //nolint:staticcheck // SA1019 — bound the deprecated field too
		return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
			"zones exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	for i, id := range r.SiteIds {
		if id <= 0 {
			return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
				"site_ids[%d] must be positive", i)
		}
	}
	statsFilter, err := fleetlistfilter.Parse(nil, r.GetTelemetryRanges())
	if err != nil {
		return collection.ListCollectionsParams{}, err
	}

	errorComponentTypes := make([]int32, len(r.ErrorComponentTypes))
	for i, ct := range r.ErrorComponentTypes {
		errorComponentTypes[i] = int32(ct)
	}

	var sort *interfaces.SortConfig
	if r.Sort != nil {
		sort = &interfaces.SortConfig{
			Field:     interfaces.SortField(r.Sort.Field),
			Direction: interfaces.SortDirection(r.Sort.Direction),
		}
	}

	zoneKeys := make([]interfaces.ZoneKey, 0, len(r.ZoneKeys)+len(r.Zones)) //nolint:staticcheck // SA1019 — intentional translation of deprecated field
	for i, zk := range r.ZoneKeys {
		if zk == nil {
			return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
				"zone_keys[%d] is nil", i)
		}
		zoneKeys = append(zoneKeys, interfaces.ZoneKey{
			BuildingID: zk.BuildingId,
			Zone:       zk.Zone,
		})
	}
	// Legacy `zones` field (deprecated): translate to wildcard ZoneKeys
	// so older clients keep working. New callers should emit zone_keys
	// directly with explicit building_id. Empty entries are rejected for
	// parity with the zone_keys.zone non-empty rule in
	// interfaces.ValidateFilterBuildings.
	for i, z := range r.Zones { //nolint:staticcheck // SA1019 — see comment above
		if z == "" {
			return collection.ListCollectionsParams{}, fleeterror.NewInvalidArgumentErrorf(
				"zones[%d] must be non-empty", i)
		}
		zoneKeys = append(zoneKeys, interfaces.ZoneKey{BuildingID: 0, Zone: z})
	}

	filter := &interfaces.DeviceSetFilter{
		ErrorComponentTypes: errorComponentTypes,
		SiteIDs:             r.SiteIds,
		IncludeUnassigned:   r.IncludeUnassigned,
		BuildingIDs:         r.BuildingIds,
		IncludeNoBuilding:   r.IncludeNoBuilding,
		ZoneKeys:            zoneKeys,
		TelemetryRanges:     statsFilter.TelemetryRanges,
	}

	return collection.ListCollectionsParams{
		Type:      toCollectionType(r.Type),
		PageSize:  r.PageSize,
		PageToken: r.PageToken,
		Sort:      sort,
		Filter:    filter,
	}, nil
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
