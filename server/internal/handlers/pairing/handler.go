package pairing

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	pairingSvc *pairing.Service
}

var _ pairingv1connect.PairingServiceHandler = &Handler{}

// NewHandler creates a new instance of Handler
func NewHandler(pairingSvc *pairing.Service) *Handler {
	return &Handler{
		pairingSvc: pairingSvc,
	}
}

// Discover implements pairingv1connect.DeviceDiscoveryServiceHandler.
func (h *Handler) Discover(ctx context.Context, r *connect.Request[pb.DiscoverRequest], s *connect.ServerStream[pb.DiscoverResponse]) error {
	slog.Debug("Discover: handling discover request", "payload", r.Msg)
	var resultChan <-chan *pb.DiscoverResponse
	var err error
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		resultChan, err = h.pairingSvc.DiscoverWithIPList(ctx, r.Msg.GetIpList())
	case *pb.DiscoverRequest_IpRange:
		resultChan, err = h.pairingSvc.DiscoverWithIPRange(ctx, r.Msg.GetIpRange())
	case *pb.DiscoverRequest_Nmap:
		resultChan, err = h.pairingSvc.DiscoverWithNmap(ctx, r.Msg.GetNmap())
	case *pb.DiscoverRequest_Mdns:
		resultChan, err = h.pairingSvc.DiscoverWithMDNS(ctx, r.Msg.GetMdns())
	default:
		return fleeterror.NewInternalError("unsupported mode")
	}

	if err != nil {
		return err
	}

	for {
		select {
		case result, ok := <-resultChan:
			if !ok {
				return nil
			}
			res := &pb.DiscoverResponse{
				Devices: result.Devices,
			}
			if err := s.Send(res); err != nil {
				// nolint:wrapcheck
				return err
			}
		case <-ctx.Done():
			return fleeterror.NewCanceledError()
		}
	}
}

// Pair implements pairingv1connect.PairingServiceHandler.
func (h *Handler) Pair(ctx context.Context, r *connect.Request[pb.PairRequest]) (*connect.Response[pb.PairResponse], error) {
	resp, err := h.pairingSvc.PairDevices(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}
