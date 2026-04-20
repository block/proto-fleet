package networkinfo

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
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

func (h Handler) UpdateNetworkNickname(context.Context, *connect.Request[pb.UpdateNetworkNicknameRequest]) (*connect.Response[pb.UpdateNetworkNicknameResponse], error) {
	// TODO implement me
	panic("unimplemented")
}
