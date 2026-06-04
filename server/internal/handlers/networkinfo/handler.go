package networkinfo

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	pairingSvc *pairing.Service
}

var _ networkinfov1connect.NetworkInfoServiceHandler = &Handler{}

func NewHandler(pairingSvc *pairing.Service) *Handler {
	return &Handler{pairingSvc: pairingSvc}
}

func (h Handler) GetNetworkInfo(ctx context.Context, _ *connect.Request[pb.GetNetworkInfoRequest]) (*connect.Response[pb.GetNetworkInfoResponse], error) {
	// GetNetworkInfo returns the device's own IP/gateway/subnet, surfaced on
	// Settings → General for every user who can view the fleet. site:read
	// would block FIELD_TECH (who can install/maintain miners but holds no
	// site-management permission), so this sits on the universal view floor.
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	info, err := h.pairingSvc.GetLocalNetworkInfo(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.GetNetworkInfoResponse{
		NetworkInfo: &pb.NetworkInfo{
			Gateway:    info.Gateway,
			LocalIp:    info.LocalIP,
			Subnet:     info.Subnet,
			LocalIpv6:  info.LocalIPv6,
			Ipv6Subnet: info.IPv6Subnet,
		},
	}), nil
}

func (h Handler) UpdateNetworkNickname(ctx context.Context, _ *connect.Request[pb.UpdateNetworkNicknameRequest]) (*connect.Response[pb.UpdateNetworkNicknameResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, fleeterror.NewUnimplementedError("UpdateNetworkNickname is not implemented")
}
