// Package maintenance is the Connect-RPC surface for MaintenanceService.
package maintenance

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/generated/grpc/maintenance/v1/maintenancev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the MaintenanceService Connect-RPC surface.
type Handler struct {
	maintenancev1connect.UnimplementedMaintenanceServiceHandler
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
	params, err := toCreateParams(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	ticket, err := h.service.CreateRepairTicket(ctx, params)
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
	markCallerComments(detail.Comments, info.UserID)
	return connect.NewResponse(&pb.GetRepairTicketResponse{Detail: &pb.RepairTicketDetail{
		Ticket:    toProtoTicket(&detail.Ticket),
		Comments:  toProtoComments(detail.Comments),
		PartsUsed: toProtoPartsUsed(detail.PartsUsed),
	}}), nil
}

func (h *Handler) ListRepairTickets(ctx context.Context, req *connect.Request[pb.ListRepairTicketsRequest]) (*connect.Response[pb.ListRepairTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	filter, err := toListFilter(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	tickets, totalCount, err := h.service.ListRepairTickets(ctx, filter)
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
	params, err := toUpdateParams(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	ticket, err := h.service.UpdateRepairTicket(ctx, params)
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

func (h *Handler) BulkUpdateRepairTickets(ctx context.Context, req *connect.Request[pb.BulkUpdateRepairTicketsRequest]) (*connect.Response[pb.BulkUpdateRepairTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if req.Msg.GetClearAssignee() && req.Msg.GetMutation() != nil {
		return nil, fleeterror.NewInvalidArgumentError("clear_assignee cannot be combined with another mutation")
	}

	var affected int64
	switch mutation := req.Msg.GetMutation().(type) {
	case *pb.BulkUpdateRepairTicketsRequest_AssignToUserId:
		assigneeUserID := mutation.AssignToUserId
		affected, err = h.service.BulkAssign(ctx, info.OrganizationID, req.Msg.GetTicketIds(), &assigneeUserID)
	case *pb.BulkUpdateRepairTicketsRequest_SetStatus:
		status, conversionErr := checkedEnumValue(int32(mutation.SetStatus), 1, 5, "set_status")
		if conversionErr != nil {
			return nil, conversionErr
		}
		affected, err = h.service.BulkUpdateStatus(
			ctx,
			info.OrganizationID,
			req.Msg.GetTicketIds(),
			models.TicketStatus(status),
		)
	case *pb.BulkUpdateRepairTicketsRequest_MarkUrgent:
		if !mutation.MarkUrgent {
			return nil, fleeterror.NewInvalidArgumentError("mark_urgent must be true")
		}
		affected, err = h.service.BulkMarkUrgent(ctx, info.OrganizationID, req.Msg.GetTicketIds())
	case *pb.BulkUpdateRepairTicketsRequest_BulkClose:
		if mutation.BulkClose == nil {
			return nil, fleeterror.NewInvalidArgumentError("bulk_close parameters are required")
		}
		params, conversionErr := toBulkCloseParams(mutation.BulkClose, info.OrganizationID, req.Msg.GetTicketIds())
		if conversionErr != nil {
			return nil, conversionErr
		}
		affected, err = h.service.BulkClose(
			ctx,
			params,
		)
	case nil:
		if !req.Msg.GetClearAssignee() {
			return nil, fleeterror.NewInvalidArgumentError("a bulk mutation is required")
		}
		affected, err = h.service.BulkAssign(ctx, info.OrganizationID, req.Msg.GetTicketIds(), nil)
	default:
		return nil, fleeterror.NewInvalidArgumentError("unsupported bulk mutation")
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkUpdateRepairTicketsResponse{
		UpdatedCount: int32(affected), //nolint:gosec // request validation caps ticket_ids at 500
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
	filter, err := toListFilter(&pb.ListRepairTicketsRequest{Filter: req.Msg.GetFilter()}, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	stats, err := h.service.GetTicketStats(ctx, filter)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toProtoTicketStats(stats)), nil
}

// ---------------------------------------------------------------
// Comments
// ---------------------------------------------------------------

func (h *Handler) ListTicketComments(ctx context.Context, req *connect.Request[pb.ListTicketCommentsRequest]) (*connect.Response[pb.ListTicketCommentsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	comments, err := h.service.ListTicketComments(ctx, info.OrganizationID, req.Msg.GetTicketId())
	if err != nil {
		return nil, err
	}
	markCallerComments(comments, info.UserID)
	return connect.NewResponse(&pb.ListTicketCommentsResponse{Comments: toProtoComments(comments)}), nil
}

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
	if err := h.service.DeleteComment(ctx, info.OrganizationID, info.UserID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteTicketCommentResponse{}), nil
}

func markCallerComments(comments []models.TicketComment, callerUserID int64) {
	for i := range comments {
		comments[i].AuthoredByCaller = comments[i].UserID == callerUserID
	}
}

// ---------------------------------------------------------------
// History
// ---------------------------------------------------------------

func (h *Handler) ListCompletedTickets(ctx context.Context, req *connect.Request[pb.ListCompletedTicketsRequest]) (*connect.Response[pb.ListCompletedTicketsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	filter, err := toCompletedFilter(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	tickets, total, err := h.service.ListCompletedTickets(ctx, filter)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListCompletedTicketsResponse(tickets, total)), nil
}

func (h *Handler) ListAssignees(ctx context.Context, _ *connect.Request[pb.ListAssigneesRequest]) (*connect.Response[pb.ListAssigneesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	assignees, err := h.service.ListAssignees(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListAssigneesResponse{Assignees: toProtoAssignees(assignees)}), nil
}
