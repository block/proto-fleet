package maintenance

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
)

// ---------------------------------------------------------------
// Proto → Domain
// ---------------------------------------------------------------

func toCreateParams(req *pb.CreateRepairTicketRequest, orgID int64) models.CreateParams {
	params := models.CreateParams{
		OrgID:          orgID,
		Category:       models.TicketCategory(req.GetCategory()),
		Urgent:         req.GetUrgent(),
		Component:      req.GetComponent(),
		WarrantyStatus: models.WarrantyStatus(req.GetWarrantyStatus()),
		DailyImpactUsd: req.GetDailyImpactUsd(),
	}
	if req.Diagnosis != nil {
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
	if req.Zone != nil {
		v := req.GetZone()
		params.Zone = &v
	}
	if req.RackId != nil {
		v := req.GetRackId()
		params.RackID = &v
	}
	if req.RackLabel != nil {
		v := req.GetRackLabel()
		params.RackLabel = &v
	}
	if req.GroupLabel != nil {
		v := req.GetGroupLabel()
		params.GroupLabel = &v
	}
	if req.Notes != nil {
		v := req.GetNotes()
		params.Notes = &v
	}
	return params
}

func toUpdateParams(req *pb.UpdateRepairTicketRequest, orgID int64) models.UpdateParams {
	params := models.UpdateParams{
		OrgID:         orgID,
		ID:            req.GetId(),
		ClearAssignee: req.GetClearAssignee(),
	}
	if req.Status != nil {
		v := models.TicketStatus(req.GetStatus())
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
		v := models.WarrantyStatus(req.GetWarrantyStatus())
		params.WarrantyStatus = &v
	}
	if req.Resolution != nil {
		v := models.TicketResolution(req.GetResolution())
		params.Resolution = &v
	}
	if req.RepairLocation != nil {
		v := models.RepairLocation(req.GetRepairLocation())
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
	return params
}

func toListFilter(req *pb.ListRepairTicketsRequest, orgID int64) models.ListFilter {
	filter := models.ListFilter{
		OrgID:            orgID,
		UrgentOnly:       req.GetUrgentOnly(),
		ExcludeCompleted: req.GetExcludeCompleted(),
		SearchQuery:      req.GetSearchQuery(),
		Limit:            req.GetLimit(),
	}
	if len(req.GetStatuses()) > 0 {
		filter.Statuses = make([]int16, len(req.GetStatuses()))
		for i, s := range req.GetStatuses() {
			filter.Statuses[i] = int16(s)
		}
	}
	if len(req.GetCategories()) > 0 {
		filter.Categories = make([]int16, len(req.GetCategories()))
		for i, c := range req.GetCategories() {
			filter.Categories[i] = int16(c)
		}
	}
	filter.SiteIDs = req.GetSiteIds()
	filter.BuildingIDs = req.GetBuildingIds()
	filter.RackIDs = req.GetRackIds()
	filter.GroupLabels = req.GetGroupLabels()
	if req.AssigneeUserId != nil {
		v := req.GetAssigneeUserId()
		filter.AssigneeUserID = &v
	}
	if req.CursorId != nil {
		v := req.GetCursorId()
		filter.CursorID = &v
	}
	return filter
}

func toCompletedFilter(req *pb.ListCompletedTicketsRequest, orgID int64) models.CompletedFilter {
	filter := models.CompletedFilter{
		OrgID: orgID,
		Limit: req.GetLimit(),
	}
	if req.Component != nil {
		v := req.GetComponent()
		filter.Component = &v
	}
	if req.AssigneeUserId != nil {
		v := req.GetAssigneeUserId()
		filter.AssigneeUserID = &v
	}
	if req.CursorId != nil {
		v := req.GetCursorId()
		filter.CursorID = &v
	}
	return filter
}

func toBulkCloseParams(req *pb.BulkCloseTicketsRequest, orgID int64) models.BulkCloseParams {
	params := models.BulkCloseParams{
		OrgID:          orgID,
		TicketIDs:      req.GetTicketIds(),
		Resolution:     models.TicketResolution(req.GetResolution()),
		RepairLocation: models.RepairLocation(req.GetRepairLocation()),
	}
	if req.Notes != nil {
		v := req.GetNotes()
		params.Notes = &v
	}
	if len(req.GetPartsUsed()) > 0 {
		params.PartsUsed = make([]models.PartUsage, len(req.GetPartsUsed()))
		for i, p := range req.GetPartsUsed() {
			params.PartsUsed[i] = models.PartUsage{
				PartName: p.GetPartName(),
				Quantity: p.GetQuantity(),
			}
		}
	}
	return params
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
		WarrantyStatus: pb.WarrantyStatus(t.WarrantyStatus),
		Resolution:     pb.TicketResolution(t.Resolution),
		RepairLocation: pb.RepairLocation(t.RepairLocation),
		DailyImpactUsd: t.DailyImpactUsd,
		CreatedAt:      timestamppb.New(t.CreatedAt),
		UpdatedAt:      timestamppb.New(t.UpdatedAt),
	}
	if t.Diagnosis != nil {
		v := *t.Diagnosis
		out.Diagnosis = &v
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
		v := *t.Notes
		out.Notes = &v
	}
	if t.RMAVendor != nil {
		v := *t.RMAVendor
		out.RmaVendor = &v
	}
	if t.RMATracking != nil {
		v := *t.RMATracking
		out.RmaTracking = &v
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
		v := *t.Zone
		out.Zone = &v
	}
	if t.RackID != nil {
		v := *t.RackID
		out.RackId = &v
	}
	if t.RackLabel != nil {
		v := *t.RackLabel
		out.RackLabel = &v
	}
	if t.GroupLabel != nil {
		v := *t.GroupLabel
		out.GroupLabel = &v
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
		Id:        c.ID,
		TicketId:  c.TicketID,
		UserId:    c.UserID,
		UserName:  c.UserName,
		Text:      c.Text,
		CreatedAt: timestamppb.New(c.CreatedAt),
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
		PartName: p.PartName,
		Quantity: p.Quantity,
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
	countByStatus := make(map[int32]int32, len(stats.CountByStatus))
	for status, count := range stats.CountByStatus {
		countByStatus[int32(status)] = count
	}
	return &pb.GetTicketStatsResponse{
		CountByStatus: countByStatus,
		Unassigned:    stats.Unassigned,
		Urgent:        stats.Urgent,
		Overdue:       stats.Overdue,
		AvgAgeHours:   stats.AvgAgeHours,
	}
}

// ---------------------------------------------------------------
// Response builders
// ---------------------------------------------------------------

func toListRepairTicketsResponse(tickets []models.RepairTicketSummary, totalCount int32) *pb.ListRepairTicketsResponse {
	out := make([]*pb.RepairTicketSummary, 0, len(tickets))
	for i := range tickets {
		out = append(out, toProtoTicketSummary(&tickets[i]))
	}
	return &pb.ListRepairTicketsResponse{
		Tickets:    out,
		TotalCount: totalCount,
	}
}

func toListCompletedTicketsResponse(tickets []models.RepairTicketSummary) *pb.ListCompletedTicketsResponse {
	out := make([]*pb.RepairTicketSummary, 0, len(tickets))
	for i := range tickets {
		out = append(out, toProtoTicketSummary(&tickets[i]))
	}
	return &pb.ListCompletedTicketsResponse{
		Tickets: out,
	}
}

func toListTicketsByMinerResponse(tickets []models.RepairTicket) *pb.ListTicketsByMinerResponse {
	out := make([]*pb.RepairTicket, 0, len(tickets))
	for i := range tickets {
		out = append(out, toProtoTicket(&tickets[i]))
	}
	return &pb.ListTicketsByMinerResponse{
		Tickets: out,
	}
}

func toListTicketsByRackResponse(tickets []models.RepairTicket) *pb.ListTicketsByRackResponse {
	out := make([]*pb.RepairTicket, 0, len(tickets))
	for i := range tickets {
		out = append(out, toProtoTicket(&tickets[i]))
	}
	return &pb.ListTicketsByRackResponse{
		Tickets: out,
	}
}
