// Package maintenance is the Connect-RPC surface for MaintenanceService.
//
// NOTE: The following permission constants must be added to
// server/internal/domain/authz/catalog.go before this handler compiles:
//
//	const (
//	    PermMaintenanceRead   = "maintenance:read"
//	    PermMaintenanceManage = "maintenance:manage"
//	)
//	const ResourceMaintenance = "maintenance"
//
// And the corresponding catalog entries:
//
//	{PermMaintenanceRead, "View repair tickets, comments, parts, and stats.", ResourceMaintenance},
//	{PermMaintenanceManage, "Create, edit, close, delete, and bulk-update repair tickets. Manage comments and parts.", ResourceMaintenance},
package maintenance

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/generated/grpc/maintenance/v1/maintenancev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the MaintenanceService Connect-RPC surface.
type Handler struct {
	service *domain.Service
}

var _ maintenancev1connect.MaintenanceServiceHandler = &Handler{}

// NewHandler returns a MaintenanceService handler bound to the supplied
// domain service.
func NewHandler(service *domain.Service) *Handler {
	return &Handler{service: service}
}

// ---------------------------------------------------------------
// Ticket CRUD
// ---------------------------------------------------------------

func (h *Handler) CreateRepairTicket(ctx context.Context, req *connect.Request[pb.CreateRepairTicketRequest]) (*connect.Response[pb.CreateRepairTicketResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	ticket, err := h.service.CreateRepairTicket(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateRepairTicketResponse{
		Ticket: toProtoTicket(ticket),
	}), nil
}

func (h *Handler) GetRepairTicket(ctx context.Context, req *connect.Request[pb.GetRepairTicketRequest]) (*connect.Response[pb.GetRepairTicketResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	detail, err := h.service.GetRepairTicket(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRepairTicketResponse{
		Ticket:   toProtoTicket(&detail.Ticket),
		Comments: toProtoComments(detail.Comments),
		Parts:    toProtoPartsUsed(detail.PartsUsed),
	}), nil
}

func (h *Handler) ListRepairTickets(ctx context.Context, req *connect.Request[pb.ListRepairTicketsRequest]) (*connect.Response[pb.ListRepairTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	tickets, totalCount, err := h.service.ListRepairTickets(ctx, toListFilter(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListRepairTicketsResponse(tickets, totalCount)), nil
}

func (h *Handler) UpdateRepairTicket(ctx context.Context, req *connect.Request[pb.UpdateRepairTicketRequest]) (*connect.Response[pb.UpdateRepairTicketResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	ticket, err := h.service.UpdateRepairTicket(ctx, toUpdateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateRepairTicketResponse{
		Ticket: toProtoTicket(ticket),
	}), nil
}

func (h *Handler) DeleteRepairTicket(ctx context.Context, req *connect.Request[pb.DeleteRepairTicketRequest]) (*connect.Response[pb.DeleteRepairTicketResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.service.DeleteRepairTicket(ctx, info.OrganizationID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteRepairTicketResponse{}), nil
}

// ---------------------------------------------------------------
// Bulk operations
// ---------------------------------------------------------------

func (h *Handler) BulkUpdateTicketStatus(ctx context.Context, req *connect.Request[pb.BulkUpdateTicketStatusRequest]) (*connect.Response[pb.BulkUpdateTicketStatusResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	affected, err := h.service.BulkUpdateStatus(
		ctx,
		info.OrganizationID,
		req.Msg.GetTicketIds(),
		models.TicketStatus(req.Msg.GetNewStatus()),
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkUpdateTicketStatusResponse{
		AffectedCount: affected,
	}), nil
}

func (h *Handler) BulkAssignTickets(ctx context.Context, req *connect.Request[pb.BulkAssignTicketsRequest]) (*connect.Response[pb.BulkAssignTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var assigneeUserID *int64
	if req.Msg.AssigneeUserId != nil {
		v := req.Msg.GetAssigneeUserId()
		assigneeUserID = &v
	}
	affected, err := h.service.BulkAssign(ctx, info.OrganizationID, req.Msg.GetTicketIds(), assigneeUserID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkAssignTicketsResponse{
		AffectedCount: affected,
	}), nil
}

func (h *Handler) BulkMarkUrgent(ctx context.Context, req *connect.Request[pb.BulkMarkUrgentRequest]) (*connect.Response[pb.BulkMarkUrgentResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	affected, err := h.service.BulkMarkUrgent(ctx, info.OrganizationID, req.Msg.GetTicketIds())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkMarkUrgentResponse{
		AffectedCount: affected,
	}), nil
}

func (h *Handler) BulkCloseTickets(ctx context.Context, req *connect.Request[pb.BulkCloseTicketsRequest]) (*connect.Response[pb.BulkCloseTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	affected, err := h.service.BulkClose(ctx, toBulkCloseParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkCloseTicketsResponse{
		AffectedCount: affected,
	}), nil
}

// ---------------------------------------------------------------
// Stats
// ---------------------------------------------------------------

func (h *Handler) GetTicketStats(ctx context.Context, req *connect.Request[pb.GetTicketStatsRequest]) (*connect.Response[pb.GetTicketStatsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	stats, err := h.service.GetTicketStats(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toProtoTicketStats(stats)), nil
}

// ---------------------------------------------------------------
// Comments
// ---------------------------------------------------------------

func (h *Handler) CreateTicketComment(ctx context.Context, req *connect.Request[pb.CreateTicketCommentRequest]) (*connect.Response[pb.CreateTicketCommentResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	comment, err := h.service.CreateComment(
		ctx,
		info.OrganizationID,
		req.Msg.GetTicketId(),
		info.UserID,
		info.Username,
		req.Msg.GetText(),
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateTicketCommentResponse{
		Comment: toProtoComment(comment),
	}), nil
}

func (h *Handler) DeleteTicketComment(ctx context.Context, req *connect.Request[pb.DeleteTicketCommentRequest]) (*connect.Response[pb.DeleteTicketCommentResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.service.DeleteComment(ctx, info.OrganizationID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteTicketCommentResponse{}), nil
}

// ---------------------------------------------------------------
// History
// ---------------------------------------------------------------

func (h *Handler) ListCompletedTickets(ctx context.Context, req *connect.Request[pb.ListCompletedTicketsRequest]) (*connect.Response[pb.ListCompletedTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	tickets, err := h.service.ListCompletedTickets(ctx, toCompletedFilter(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListCompletedTicketsResponse(tickets)), nil
}

// ---------------------------------------------------------------
// Miner / Rack scoped
// ---------------------------------------------------------------

func (h *Handler) ListTicketsByMiner(ctx context.Context, req *connect.Request[pb.ListTicketsByMinerRequest]) (*connect.Response[pb.ListTicketsByMinerResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	tickets, err := h.service.ListTicketsByMiner(ctx, info.OrganizationID, req.Msg.GetMinerIdentifier())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListTicketsByMinerResponse(tickets)), nil
}

func (h *Handler) ListTicketsByRack(ctx context.Context, req *connect.Request[pb.ListTicketsByRackRequest]) (*connect.Response[pb.ListTicketsByRackResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	tickets, err := h.service.ListTicketsByRack(ctx, info.OrganizationID, req.Msg.GetRackId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListTicketsByRackResponse(tickets)), nil
}
