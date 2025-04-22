package pairing

import (
	"context"
	"errors"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain/pairing"
	"log/slog"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/miner-firmware/fleet/generated/grpc/pairing/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/pairing/v1/pairingv1connect"
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
	var resultChan <-chan *pairing.DiscoveryResponse
	var err error
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		req := &pairing.IPListDiscoveryRequest{
			IPAddresses:    r.Msg.GetIpList().IpAddresses,
			Ports:          r.Msg.GetIpList().Ports,
			TimeoutSeconds: r.Msg.GetIpList().TimeoutSeconds,
		}
		resultChan, err = h.pairingSvc.DiscoverWithIPList(ctx, req)
	case *pb.DiscoverRequest_IpRange:
		req := &pairing.IPRangeDiscoveryRequest{
			StartIP:        r.Msg.GetIpRange().StartIp,
			EndIP:          r.Msg.GetIpRange().EndIp,
			Ports:          r.Msg.GetIpRange().Ports,
			TimeoutSeconds: r.Msg.GetIpRange().TimeoutSeconds,
		}
		resultChan, err = h.pairingSvc.DiscoverWithIPRange(ctx, req)
	case *pb.DiscoverRequest_Nmap:
		req := &pairing.NmapDiscoveryRequest{
			Target:   r.Msg.GetNmap().Target,
			Ports:    r.Msg.GetNmap().Ports,
			FastScan: r.Msg.GetNmap().FastScan,
		}
		resultChan, err = h.pairingSvc.DiscoverWithNmap(ctx, req)
	case *pb.DiscoverRequest_Mdns:
		req := &pairing.MDNSDiscoveryRequest{
			ServiceType:    r.Msg.GetMdns().ServiceType,
			Domain:         r.Msg.GetMdns().Domain,
			TimeoutSeconds: r.Msg.GetMdns().TimeoutSeconds,
		}
		resultChan, err = h.pairingSvc.DiscoverWithMDNS(ctx, req)
	default:
		return connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported mode"))
	}

	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}

	for {
		select {
		case result, ok := <-resultChan:
			if !ok {
				return nil
			}

			if err := s.Send(toDiscoveryResponse(result.Devices)); err != nil {
				return connect.NewError(connect.CodeInternal, err)
			}
		case <-ctx.Done():
			return connect.NewError(connect.CodeCanceled, ctx.Err())
		}
	}
}

func toDiscoveryResponse(d []*pairing.Device) *pb.DiscoverResponse {
	var devices []*pb.Device
	for _, d := range d {
		devices = append(devices, &pb.Device{
			IpAddress:    d.IPAddress,
			MacAddress:   d.MacAddress,
			Hostname:     d.Hostname,
			DiscoveredAt: d.DiscoveredAt,
		})
	}
	res := &pb.DiscoverResponse{
		Devices: devices,
	}
	return res
}

// Pair implements pairingv1connect.PairingServiceHandler.
func (h *Handler) Pair(context.Context, *connect.Request[pb.PairRequest]) (*connect.Response[pb.PairResponse], error) {
	panic("unimplemented")
}
