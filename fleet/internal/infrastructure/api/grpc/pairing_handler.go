package grpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/miner-firmware/fleet/generated/grpc/pairing/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
)

// DeviceDiscoveryHandler handles the Connect-RPC endpoints
type DeviceDiscoveryHandler struct {
	discoveryService *domain.PairingService
}

var _ pairingv1connect.PairingServiceHandler = &DeviceDiscoveryHandler{}

// NewDeviceDiscoveryHandler creates a new instance of DeviceDiscoveryHandler
func NewDeviceDiscoveryHandler(discoveryService *domain.PairingService) *DeviceDiscoveryHandler {
	return &DeviceDiscoveryHandler{
		discoveryService: discoveryService,
	}
}

// Discover implements pairingv1connect.DeviceDiscoveryServiceHandler.
func (h *DeviceDiscoveryHandler) Discover(ctx context.Context, r *connect.Request[pb.DiscoverRequest], s *connect.ServerStream[pb.DiscoverResponse]) error {
	var resultChan <-chan *domain.DiscoveryResponse
	var err error
	switch r.Msg.Mode.(type) {
	case *pb.DiscoverRequest_IpList:
		req := &domain.IPListDiscoveryRequest{
			IPAddresses:    r.Msg.GetIpList().IpAddresses,
			Ports:          r.Msg.GetIpList().Ports,
			TimeoutSeconds: r.Msg.GetIpList().TimeoutSeconds,
		}
		resultChan, err = h.discoveryService.DiscoverWithIPList(ctx, req)
	case *pb.DiscoverRequest_IpRange:
		req := &domain.IPRangeDiscoveryRequest{
			StartIP:        r.Msg.GetIpRange().StartIp,
			EndIP:          r.Msg.GetIpRange().EndIp,
			Ports:          r.Msg.GetIpRange().Ports,
			TimeoutSeconds: r.Msg.GetIpRange().TimeoutSeconds,
		}
		resultChan, err = h.discoveryService.DiscoverWithIPRange(ctx, req)
	case *pb.DiscoverRequest_Nmap:
		req := &domain.NmapDiscoveryRequest{
			Target:   r.Msg.GetNmap().Target,
			Ports:    r.Msg.GetNmap().Ports,
			FastScan: r.Msg.GetNmap().FastScan,
		}
		resultChan, err = h.discoveryService.DiscoverWithNmap(ctx, req)
	case *pb.DiscoverRequest_Mdns:
		req := &domain.MDNSDiscoveryRequest{
			ServiceType:    r.Msg.GetMdns().ServiceType,
			TimeoutSeconds: r.Msg.GetMdns().TimeoutSeconds,
		}
		resultChan, err = h.discoveryService.DiscoverWithMDNS(ctx, req)
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

func toDiscoveryResponse(d []*domain.Device) *pb.DiscoverResponse {
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
func (h *DeviceDiscoveryHandler) Pair(context.Context, *connect.Request[pb.PairRequest]) (*connect.Response[pb.PairResponse], error) {
	panic("unimplemented")
}
