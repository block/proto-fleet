package maintenance

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
)

// ---------------------------------------------------------------
// Proto → Domain
// ---------------------------------------------------------------

func toCreateParams(req *pb.CreateRepairTicketRequest, orgID int64) (models.CreateParams, error) {
	category, err := checkedEnumValue(int32(req.GetCategory()), 1, 2, "category")
	if err != nil {
		return models.CreateParams{}, err
	}
	warrantyStatus, err := checkedEnumValue(int32(req.GetWarrantyStatus()), 0, 3, "warranty_status")
	if err != nil {
		return models.CreateParams{}, err
	}
	params := models.CreateParams{
		OrgID:          orgID,
		Category:       models.TicketCategory(category),
		Urgent:         req.GetUrgent(),
		Component:      req.GetComponent(),
		WarrantyStatus: models.WarrantyStatus(warrantyStatus),
	}
	if req.GetDiagnosis() != "" {
		v := req.GetDiagnosis()
		params.Diagnosis = &v
	}
	if req.MinerIdentifier != nil {
		v := req.GetMinerIdentifier()
		params.MinerIdentifier = &v
	}
	if req.AlertId != nil {
		v := req.GetAlertId()
		params.AlertID = &v
	}
	if req.AssigneeUserId != nil {
		v := req.GetAssigneeUserId()
		params.AssigneeUserID = &v
	}
	if req.SiteId != nil {
		v := req.GetSiteId()
		params.SiteID = &v
	}
	if req.BuildingId != nil {
		v := req.GetBuildingId()
		params.BuildingID = &v
	}
	if req.GetZone() != "" {
		v := req.GetZone()
		params.Zone = &v
	}
	if req.RackId != nil {
		v := req.GetRackId()
		params.RackID = &v
	}
	if req.GetRackLabel() != "" {
		v := req.GetRackLabel()
		params.RackLabel = &v
	}
	if req.GetGroupLabel() != "" {
		v := req.GetGroupLabel()
		params.GroupLabel = &v
	}
	if req.GetNotes() != "" {
		v := req.GetNotes()
		params.Notes = &v
	}
	return params, nil
}

func toUpdateParams(req *pb.UpdateRepairTicketRequest, orgID int64) (models.UpdateParams, error) {
	params := models.UpdateParams{
		OrgID:         orgID,
		ID:            req.GetId(),
		ClearAssignee: req.GetClearAssignee(),
		ClearRMAEta:   req.GetClearRmaEta(),
	}
	if req.Status != nil {
		raw, err := checkedEnumValue(int32(req.GetStatus()), 1, 5, "status")
		if err != nil {
			return models.UpdateParams{}, err
		}
		v := models.TicketStatus(raw)
		params.Status = &v
	}
	if req.Urgent != nil {
		v := req.GetUrgent()
		params.Urgent = &v
	}
	if req.AssigneeUserId != nil {
		v := req.GetAssigneeUserId()
		params.AssigneeUserID = &v
	}
	if req.Component != nil {
		v := req.GetComponent()
		params.Component = &v
	}
	if req.Diagnosis != nil {
		v := req.GetDiagnosis()
		params.Diagnosis = &v
	}
	if req.WarrantyStatus != nil {
		raw, err := checkedEnumValue(int32(req.GetWarrantyStatus()), 0, 3, "warranty_status")
		if err != nil {
			return models.UpdateParams{}, err
		}
		v := models.WarrantyStatus(raw)
		params.WarrantyStatus = &v
	}
	if req.Resolution != nil {
		raw, err := checkedEnumValue(int32(req.GetResolution()), 0, 5, "resolution")
		if err != nil {
			return models.UpdateParams{}, err
		}
		v := models.TicketResolution(raw)
		params.Resolution = &v
	}
	if req.RepairLocation != nil {
		raw, err := checkedEnumValue(int32(req.GetRepairLocation()), 0, 2, "repair_location")
		if err != nil {
			return models.UpdateParams{}, err
		}
		v := models.RepairLocation(raw)
		params.RepairLocation = &v
	}
	if req.Notes != nil {
		v := req.GetNotes()
		params.Notes = &v
	}
	if req.RmaVendor != nil {
		v := req.GetRmaVendor()
		params.RMAVendor = &v
	}
	if req.RmaTracking != nil {
		v := req.GetRmaTracking()
		params.RMATracking = &v
	}
	if req.RmaEta != nil {
		t := req.GetRmaEta().AsTime()
		params.RMAEta = &t
	}
	if req.PartsSelection != nil {
		selected := req.GetPartsSelection().GetParts()
		parts := make([]models.PartUsage, len(selected))
		for i, part := range selected {
			parts[i] = models.PartUsage{
				InventoryPartID: part.GetInventoryPartId(),
				PartName:        part.GetPartName(),
				Quantity:        part.GetQuantity(),
			}
		}
		params.PartsSelection = &parts
	}
	return params, nil
}

func toListFilter(req *pb.ListRepairTicketsRequest, orgID int64) (models.ListFilter, error) {
	requestFilter := req.GetFilter()
	sortField, err := checkedEnumValue(int32(req.GetSortField()), 0, 5, "sort_field")
	if err != nil {
		return models.ListFilter{}, err
	}
	sortDirection, err := checkedEnumValue(int32(req.GetSortDirection()), 0, 2, "sort_direction")
	if err != nil {
		return models.ListFilter{}, err
	}
	filter := models.ListFilter{
		OrgID:            orgID,
		UrgentOnly:       requestFilter.GetUrgentOnly(),
		ExcludeCompleted: requestFilter.GetExcludeCompleted(),
		OverdueOnly:      requestFilter.GetOverdueOnly(),
		SearchQuery:      requestFilter.GetSearchQuery(),
		SortField:        models.TicketSortField(sortField),
		SortDirection:    models.SortDirection(sortDirection),
		Limit:            req.GetPageSize(),
	}
	if len(requestFilter.GetStatuses()) > 0 {
		filter.Statuses = make([]int16, len(requestFilter.GetStatuses()))
		for i, s := range requestFilter.GetStatuses() {
			value, err := checkedEnumValue(int32(s), 1, 5, "filter.statuses")
			if err != nil {
				return models.ListFilter{}, err
			}
			filter.Statuses[i] = value
		}
	}
	if len(requestFilter.GetCategories()) > 0 {
		filter.Categories = make([]int16, len(requestFilter.GetCategories()))
		for i, c := range requestFilter.GetCategories() {
			value, err := checkedEnumValue(int32(c), 1, 2, "filter.categories")
			if err != nil {
				return models.ListFilter{}, err
			}
			filter.Categories[i] = value
		}
	}
	filter.SiteIDs = requestFilter.GetSiteIds()
	filter.BuildingIDs = requestFilter.GetBuildingIds()
	filter.RackIDs = requestFilter.GetRackIds()
	filter.GroupLabels = requestFilter.GetGroupLabels()
	if requestFilter != nil && requestFilter.AssigneeUserId != nil {
		v := requestFilter.GetAssigneeUserId()
		filter.AssigneeUserID = &v
	}
	if req.GetPageToken() != "" {
		cursor, err := models.DecodeTicketCursor(req.GetPageToken())
		if err != nil {
			return models.ListFilter{}, fleeterror.NewInvalidArgumentError("invalid page_token")
		}
		filter.Cursor = &cursor
	}
	return filter, nil
}

func toCompletedFilter(req *pb.ListCompletedTicketsRequest, orgID int64) (models.CompletedFilter, error) {
	sortField, err := checkedEnumValue(int32(req.GetSortField()), 0, 6, "sort_field")
	if err != nil {
		return models.CompletedFilter{}, err
	}
	sortDirection, err := checkedEnumValue(int32(req.GetSortDirection()), 0, 2, "sort_direction")
	if err != nil {
		return models.CompletedFilter{}, err
	}
	filter := models.CompletedFilter{
		OrgID:         orgID,
		SortField:     models.TicketSortField(sortField),
		SortDirection: models.SortDirection(sortDirection),
		Limit:         req.GetPageSize(),
	}
	if req.ComponentFilter != nil {
		v := req.GetComponentFilter()
		filter.Component = &v
	}
	if req.AssigneeUserIdFilter != nil {
		v := req.GetAssigneeUserIdFilter()
		filter.AssigneeUserID = &v
	}
	if req.GetPageToken() != "" {
		cursor, err := models.DecodeTicketCursor(req.GetPageToken())
		if err != nil {
			return models.CompletedFilter{}, fleeterror.NewInvalidArgumentError("invalid page_token")
		}
		filter.Cursor = &cursor
	}
	return filter, nil
}

func checkedEnumValue(value, minValue, maxValue int32, field string) (int16, error) {
	if value < minValue || value > maxValue {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid %s", field)
	}
	return int16(value), nil //nolint:gosec // explicit range check above proves the conversion is safe
}

func toBulkCloseParams(req *pb.BulkCloseParams, orgID int64, ticketIDs []int64) (models.BulkCloseParams, error) {
	resolution, err := checkedEnumValue(int32(req.GetResolution()), 1, 5, "bulk_close.resolution")
	if err != nil {
		return models.BulkCloseParams{}, err
	}
	repairLocation, err := checkedEnumValue(int32(req.GetRepairLocation()), 0, 2, "bulk_close.repair_location")
	if err != nil {
		return models.BulkCloseParams{}, err
	}
	params := models.BulkCloseParams{
		OrgID:          orgID,
		TicketIDs:      ticketIDs,
		Resolution:     models.TicketResolution(resolution),
		RepairLocation: models.RepairLocation(repairLocation),
	}
	if req.GetNotes() != "" {
		v := req.GetNotes()
		params.Notes = &v
	}
	return params, nil
}

// ---------------------------------------------------------------
// Domain → Proto
// ---------------------------------------------------------------

func toProtoTicket(t *models.RepairTicket) *pb.RepairTicket {
	if t == nil {
		return nil
	}
	out := &pb.RepairTicket{
		Id:             t.ID,
		TicketNumber:   t.TicketNumber,
		Category:       pb.TicketCategory(t.Category),
		Status:         pb.TicketStatus(t.Status),
		Urgent:         t.Urgent,
		Component:      t.Component,
		AssigneeName:   t.AssigneeName,
		WarrantyStatus: pb.WarrantyStatus(t.WarrantyStatus),
		Resolution:     pb.TicketResolution(t.Resolution),
		RepairLocation: pb.RepairLocation(t.RepairLocation),
		DailyImpactUsd: t.DailyImpactUsd,
		SiteName:       t.SiteName,
		BuildingName:   t.BuildingName,
		CreatedAt:      timestamppb.New(t.CreatedAt),
		UpdatedAt:      timestamppb.New(t.UpdatedAt),
	}
	if t.Diagnosis != nil {
		out.Diagnosis = *t.Diagnosis
	}
	if t.MinerIdentifier != nil {
		v := *t.MinerIdentifier
		out.MinerIdentifier = &v
	}
	if t.AlertID != nil {
		v := *t.AlertID
		out.AlertId = &v
	}
	if t.AssigneeUserID != nil {
		v := *t.AssigneeUserID
		out.AssigneeUserId = &v
	}
	if t.Notes != nil {
		out.Notes = *t.Notes
	}
	if t.RMAVendor != nil {
		out.RmaVendor = *t.RMAVendor
	}
	if t.RMATracking != nil {
		out.RmaTracking = *t.RMATracking
	}
	if t.RMAEta != nil {
		out.RmaEta = timestamppb.New(*t.RMAEta)
	}
	if t.SiteID != nil {
		v := *t.SiteID
		out.SiteId = &v
	}
	if t.BuildingID != nil {
		v := *t.BuildingID
		out.BuildingId = &v
	}
	if t.Zone != nil {
		out.Zone = *t.Zone
	}
	if t.RackID != nil {
		v := *t.RackID
		out.RackId = &v
	}
	if t.RackLabel != nil {
		out.RackLabel = *t.RackLabel
	}
	if t.GroupLabel != nil {
		out.GroupLabel = *t.GroupLabel
	}
	if t.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*t.CompletedAt)
	}
	return out
}

func toProtoTicketSummary(s *models.RepairTicketSummary) *pb.RepairTicketSummary {
	if s == nil {
		return nil
	}
	return &pb.RepairTicketSummary{
		Ticket:       toProtoTicket(&s.RepairTicket),
		CommentCount: s.CommentCount,
		PartsCount:   s.PartsCount,
	}
}

func toProtoComment(c *models.TicketComment) *pb.TicketComment {
	if c == nil {
		return nil
	}
	return &pb.TicketComment{
		Id:               c.ID,
		TicketId:         c.TicketID,
		UserId:           c.UserID,
		UserName:         c.UserName,
		Text:             c.Text,
		AuthoredByCaller: c.AuthoredByCaller,
		CreatedAt:        timestamppb.New(c.CreatedAt),
	}
}

func toProtoComments(comments []models.TicketComment) []*pb.TicketComment {
	out := make([]*pb.TicketComment, 0, len(comments))
	for i := range comments {
		out = append(out, toProtoComment(&comments[i]))
	}
	return out
}

func toProtoPartUsage(p *models.PartUsage) *pb.PartUsage {
	if p == nil {
		return nil
	}
	return &pb.PartUsage{
		InventoryPartId: p.InventoryPartID,
		PartName:        p.PartName,
		Quantity:        p.Quantity,
	}
}

func toProtoPartsUsed(parts []models.PartUsage) []*pb.PartUsage {
	out := make([]*pb.PartUsage, 0, len(parts))
	for i := range parts {
		out = append(out, toProtoPartUsage(&parts[i]))
	}
	return out
}

func toProtoTicketStats(stats *models.TicketStats) *pb.GetTicketStatsResponse {
	if stats == nil {
		return nil
	}
	return &pb.GetTicketStatsResponse{
		OpenCount:         stats.CountByStatus[models.TicketStatusOpen],
		InProgressCount:   stats.CountByStatus[models.TicketStatusInProgress],
		OnHoldCount:       stats.CountByStatus[models.TicketStatusOnHold],
		SentToVendorCount: stats.CountByStatus[models.TicketStatusSentToVendor],
		CompletedCount:    stats.CountByStatus[models.TicketStatusCompleted],
		UnassignedCount:   stats.Unassigned,
		UrgentCount:       stats.Urgent,
		OverdueCount:      stats.Overdue,
		AvgAgeHours:       stats.AvgAgeHours,
	}
}

// ---------------------------------------------------------------
// Response builders
// ---------------------------------------------------------------

func toListRepairTicketsResponse(tickets []models.RepairTicketSummary, totalCount int32, hasNext bool) *pb.ListRepairTicketsResponse {
	out := make([]*pb.RepairTicketSummary, 0, len(tickets))
	for i := range tickets {
		out = append(out, toProtoTicketSummary(&tickets[i]))
	}
	return &pb.ListRepairTicketsResponse{
		Tickets:       out,
		NextPageToken: nextPageToken(tickets, hasNext),
		TotalCount:    totalCount,
	}
}

func toListCompletedTicketsResponse(tickets []models.RepairTicketSummary, totalCount int32, hasNext bool) *pb.ListCompletedTicketsResponse {
	out := make([]*pb.RepairTicketSummary, 0, len(tickets))
	for i := range tickets {
		out = append(out, toProtoTicketSummary(&tickets[i]))
	}
	return &pb.ListCompletedTicketsResponse{
		Tickets:       out,
		NextPageToken: nextPageToken(tickets, hasNext),
		TotalCount:    totalCount,
	}
}

func toProtoAssignees(assignees []models.Assignee) []*pb.Assignee {
	out := make([]*pb.Assignee, 0, len(assignees))
	for _, assignee := range assignees {
		out = append(out, &pb.Assignee{UserId: assignee.UserID, Username: assignee.Username, RoleName: assignee.RoleName})
	}
	return out
}

func nextPageToken(tickets []models.RepairTicketSummary, hasNext bool) string {
	if !hasNext || len(tickets) == 0 {
		return ""
	}
	token, err := tickets[len(tickets)-1].Cursor.Encode()
	if err != nil {
		return ""
	}
	return token
}
