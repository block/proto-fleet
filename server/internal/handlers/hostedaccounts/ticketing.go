package hostedaccounts

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
)

func (h *Handler) ListAccountTickets(ctx context.Context, req *connect.Request[pb.ListAccountTicketsRequest]) (*connect.Response[pb.ListAccountTicketsResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result, err := h.accounts.ListAccountTickets(ctx, info.UserID, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) ListAccountTicketAssets(ctx context.Context, req *connect.Request[pb.ListAccountTicketAssetsRequest]) (*connect.Response[pb.ListAccountTicketAssetsResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result, err := h.accounts.ListAccountTicketAssets(ctx, info.UserID, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) GetAccountTicket(ctx context.Context, req *connect.Request[pb.GetAccountTicketRequest]) (*connect.Response[pb.GetAccountTicketResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	detail, err := h.accounts.GetAccountTicket(ctx, info.UserID, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetAccountTicketResponse{Detail: detail}), nil
}

func (h *Handler) CreateAccountTicket(ctx context.Context, req *connect.Request[pb.CreateAccountTicketRequest]) (*connect.Response[pb.CreateAccountTicketResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	detail, err := h.accounts.CreateAccountTicket(ctx, info.UserID, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateAccountTicketResponse{Detail: detail}), nil
}

func (h *Handler) CommentAccountTicket(ctx context.Context, req *connect.Request[pb.CommentAccountTicketRequest]) (*connect.Response[pb.CommentAccountTicketResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	detail, err := h.accounts.CommentAccountTicket(ctx, info.UserID, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CommentAccountTicketResponse{Detail: detail}), nil
}

func (h *Handler) ChangeAccountTicketStatus(ctx context.Context, req *connect.Request[pb.ChangeAccountTicketStatusRequest]) (*connect.Response[pb.ChangeAccountTicketStatusResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	detail, err := h.accounts.ChangeAccountTicketStatus(ctx, info.UserID, info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ChangeAccountTicketStatusResponse{Detail: detail}), nil
}
