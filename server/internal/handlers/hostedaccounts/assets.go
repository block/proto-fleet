package hostedaccounts

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
)

func (h *Handler) ReplaceAccountHashboard(ctx context.Context, request *connect.Request[pb.ReplaceAccountHashboardRequest]) (*connect.Response[pb.ReplaceAccountHashboardResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err := h.accounts.ReplaceHashboard(ctx, info.UserID, info.OrganizationID, request.Msg); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReplaceAccountHashboardResponse{}), nil
}

func (h *Handler) GetAccountMiningSummary(ctx context.Context, request *connect.Request[pb.GetAccountMiningSummaryRequest]) (*connect.Response[pb.GetAccountMiningSummaryResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result, err := h.accounts.MiningSummary(ctx, info.UserID, info.OrganizationID, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) ListAccountAssets(ctx context.Context, request *connect.Request[pb.ListAccountAssetsRequest]) (*connect.Response[pb.ListAccountAssetsResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result, err := h.accounts.ListAssets(ctx, info.UserID, info.OrganizationID, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) AssignAccountAssets(ctx context.Context, request *connect.Request[pb.AssignAccountAssetsRequest]) (*connect.Response[pb.AssignAccountAssetsResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.AssignAssets(ctx, info.UserID, info.OrganizationID, request.Msg); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignAccountAssetsResponse{}), nil
}

func (h *Handler) GetAccountDashboard(ctx context.Context, request *connect.Request[pb.GetAccountDashboardRequest]) (*connect.Response[pb.GetAccountDashboardResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result, err := h.accounts.Dashboard(ctx, info.UserID, info.OrganizationID, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) GetAccountAssetDetail(ctx context.Context, request *connect.Request[pb.GetAccountAssetDetailRequest]) (*connect.Response[pb.GetAccountAssetDetailResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	result, err := h.accounts.AssetDetail(ctx, info.UserID, info.OrganizationID, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
