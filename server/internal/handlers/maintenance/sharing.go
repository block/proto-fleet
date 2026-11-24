package maintenance

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func (h *Handler) ShareRepairTicket(ctx context.Context, req *connect.Request[pb.ShareRepairTicketRequest]) (*connect.Response[pb.ShareRepairTicketResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	if req.Msg.ExpectedUpdatedAt == nil || req.Msg.ExpectedUpdatedAt.CheckValid() != nil {
		return nil, fleeterror.NewInvalidArgumentError("expected_updated_at is required")
	}
	ticket, err := h.service.ShareRepairTicket(ctx, info.OrganizationID, req.Msg.TicketId, req.Msg.ExpectedUpdatedAt.AsTime())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ShareRepairTicketResponse{Ticket: toProtoTicket(ticket)}), nil
}
func (h *Handler) SetTicketCommentVisibility(ctx context.Context, req *connect.Request[pb.SetTicketCommentVisibilityRequest]) (*connect.Response[pb.SetTicketCommentVisibilityResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	if req.Msg.Internal == nil {
		return nil, fleeterror.NewInvalidArgumentError("internal is required")
	}
	if req.Msg.ExpectedUpdatedAt == nil || req.Msg.ExpectedUpdatedAt.CheckValid() != nil {
		return nil, fleeterror.NewInvalidArgumentError("expected_updated_at is required")
	}
	if err := h.service.SetTicketCommentVisibility(ctx, info.OrganizationID, req.Msg.TicketId, req.Msg.CommentId, req.Msg.GetInternal(), req.Msg.ExpectedUpdatedAt.AsTime()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetTicketCommentVisibilityResponse{}), nil
}
