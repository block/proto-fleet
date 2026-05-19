package fleetnodeadmin

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodecontrol"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodepairing"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment *fleetnodeenrollment.Service
	pairing    *fleetnodepairing.Service
	registry   *fleetnodecontrol.Registry
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(enrollment *fleetnodeenrollment.Service, pairing *fleetnodepairing.Service, registry *fleetnodecontrol.Registry) *Handler {
	return &Handler{enrollment: enrollment, pairing: pairing, registry: registry}
}

func (h *Handler) CreateEnrollmentCode(ctx context.Context, _ *connect.Request[pb.CreateEnrollmentCodeRequest]) (*connect.Response[pb.CreateEnrollmentCodeResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	code, expiresAt, err := h.enrollment.CreateCode(ctx, info.UserID, info.OrganizationID, 0)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateEnrollmentCodeResponse{
		Code:      code,
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) ListFleetNodes(ctx context.Context, _ *connect.Request[pb.ListFleetNodesRequest]) (*connect.Response[pb.ListFleetNodesResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	fleetNodes, err := h.enrollment.ListFleetNodes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodesResponse{FleetNodes: make([]*pb.FleetNodeSummary, 0, len(fleetNodes))}
	for _, n := range fleetNodes {
		summary := &pb.FleetNodeSummary{
			FleetNodeId:         n.ID,
			Name:                n.Name,
			EnrollmentStatus:    deriveDisplayStatus(n),
			IdentityFingerprint: fleetnodeenrollment.IdentityFingerprint(n.IdentityPubkey),
			CreatedAt:           timestamppb.New(n.CreatedAt),
		}
		if n.LastSeenAt != nil {
			summary.LastSeenAt = timestamppb.New(*n.LastSeenAt)
		}
		resp.FleetNodes = append(resp.FleetNodes, summary)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ConfirmFleetNode(ctx context.Context, req *connect.Request[pb.ConfirmFleetNodeRequest]) (*connect.Response[pb.ConfirmFleetNodeResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	apiKey, expiresAt, err := h.enrollment.Confirm(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ConfirmFleetNodeResponse{ApiKey: apiKey}
	if !expiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(expiresAt)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RevokeFleetNode(ctx context.Context, req *connect.Request[pb.RevokeFleetNodeRequest]) (*connect.Response[pb.RevokeFleetNodeResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.enrollment.RevokeFleetNode(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeFleetNodeResponse{}), nil
}

func (h *Handler) PairDeviceToFleetNode(ctx context.Context, req *connect.Request[pb.PairDeviceToFleetNodeRequest]) (*connect.Response[pb.PairDeviceToFleetNodeResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	assignedBy := info.UserID
	if err := h.pairing.PairDevice(ctx, req.Msg.GetFleetNodeId(), req.Msg.GetDeviceId(), info.OrganizationID, &assignedBy); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PairDeviceToFleetNodeResponse{}), nil
}

func (h *Handler) UnpairDevice(ctx context.Context, req *connect.Request[pb.UnpairDeviceRequest]) (*connect.Response[pb.UnpairDeviceResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.pairing.UnpairDevice(ctx, req.Msg.GetDeviceId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UnpairDeviceResponse{}), nil
}

func (h *Handler) ListFleetNodeDevices(ctx context.Context, req *connect.Request[pb.ListFleetNodeDevicesRequest]) (*connect.Response[pb.ListFleetNodeDevicesResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	var pairs []fleetnodepairing.FleetNodeDevice
	if fleetNodeID := req.Msg.GetFleetNodeId(); fleetNodeID > 0 {
		pairs, err = h.pairing.ListDevicesForFleetNode(ctx, fleetNodeID, info.OrganizationID)
	} else {
		pairs, err = h.pairing.ListPairs(ctx, info.OrganizationID)
	}
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodeDevicesResponse{Pairs: make([]*pb.FleetNodeDeviceSummary, 0, len(pairs))}
	for _, p := range pairs {
		summary := &pb.FleetNodeDeviceSummary{
			FleetNodeId:      p.FleetNodeID,
			DeviceId:         p.DeviceID,
			DeviceIdentifier: p.DeviceIdentifier,
			DeviceType:       p.DeviceType,
			AssignedAt:       timestamppb.New(p.AssignedAt),
		}
		if p.AssignedBy != nil {
			summary.AssignedBy = p.AssignedBy
		}
		resp.Pairs = append(resp.Pairs, summary)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) DiscoverOnFleetNode(ctx context.Context, req *connect.Request[pb.DiscoverOnFleetNodeRequest], stream *connect.ServerStream[pb.DiscoverOnFleetNodeResponse]) error {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return err
	}
	fleetNodeID := req.Msg.GetFleetNodeId()
	if fleetNodeID <= 0 {
		return fleeterror.NewInvalidArgumentError("fleet_node_id is required")
	}
	discoverReq := req.Msg.GetRequest()
	if discoverReq == nil {
		return fleeterror.NewInvalidArgumentError("request is required")
	}

	node, err := h.enrollment.GetFleetNodeByID(ctx, fleetNodeID, info.OrganizationID)
	if err != nil {
		return err
	}
	if node.EnrollmentStatus != fleetnodeenrollment.FleetNodeStatusConfirmed {
		return fleeterror.NewFailedPreconditionError("fleet node is not CONFIRMED")
	}

	normalized, err := normalizeDiscoverRequest(discoverReq)
	if err != nil {
		return err
	}

	commandID, err := newCommandID()
	if err != nil {
		return fleeterror.NewInternalErrorf("generate command_id: %v", err)
	}
	payload, err := proto.Marshal(normalized)
	if err != nil {
		return fleeterror.NewInternalErrorf("marshal discover payload: %v", err)
	}

	events, cleanup, err := h.registry.Send(ctx, fleetNodeID, &gatewaypb.ControlCommand{
		CommandId: commandID,
		Payload:   payload,
	})
	if err != nil {
		if errors.Is(err, fleetnodecontrol.ErrNoActiveStream) {
			return fleeterror.NewFailedPreconditionError("fleet node has no active control stream")
		}
		return err
	}
	defer cleanup()

	for {
		select {
		case <-ctx.Done():
			return fleeterror.NewInternalErrorf("operator stream cancelled: %v", ctx.Err())
		case ev, ok := <-events:
			if !ok {
				return fleeterror.NewFailedPreconditionError("fleet node control stream closed before command completed")
			}
			if ev.Batch != nil {
				if sendErr := stream.Send(&pb.DiscoverOnFleetNodeResponse{Response: ev.Batch}); sendErr != nil {
					return fleeterror.NewInternalErrorf("send batch to operator: %v", sendErr)
				}
			}
			if ev.Ack != nil {
				if msg := ev.Ack.GetErrorMessage(); !ev.Ack.GetSucceeded() && msg != "" {
					return fleeterror.NewInternalErrorf("fleet node reported discovery failure: %s", msg)
				}
				return nil
			}
		}
	}
}

func newCommandID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fleeterror.NewInternalErrorf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func normalizeDiscoverRequest(in *pairingpb.DiscoverRequest) (*pairingpb.DiscoverRequest, error) {
	switch m := in.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		if m.IpList == nil || len(m.IpList.GetIpAddresses()) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("ip_list.ip_addresses must not be empty")
		}
		return in, nil
	case *pairingpb.DiscoverRequest_IpRange:
		ips, err := expandIPv4Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp())
		if err != nil {
			return nil, err
		}
		return &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{
					IpAddresses: ips,
					Ports:       m.IpRange.GetPorts(),
				},
			},
		}, nil
	case *pairingpb.DiscoverRequest_Nmap:
		if m.Nmap == nil || m.Nmap.GetTarget() == "" {
			return nil, fleeterror.NewInvalidArgumentError("nmap.target must not be empty")
		}
		return in, nil
	case *pairingpb.DiscoverRequest_Mdns:
		return nil, fleeterror.NewInvalidArgumentError("mdns discovery is not supported on fleet nodes")
	default:
		return nil, fleeterror.NewInvalidArgumentError("discover request mode is required")
	}
}

// Cap on the marshalled ControlCommand payload so a slow operator can't
// queue an arbitrarily large scan against a single fleet node.
const maxExpandedIPs = 4096

func expandIPv4Range(startStr, endStr string) ([]string, error) {
	start, err := parseIPv4(startStr)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid start_ip: %v", err)
	}
	end, err := parseIPv4(endStr)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid end_ip: %v", err)
	}
	if end < start {
		return nil, fleeterror.NewInvalidArgumentError("end_ip must be >= start_ip")
	}
	if end-start+1 > maxExpandedIPs {
		return nil, fleeterror.NewInvalidArgumentErrorf("ip range exceeds %d addresses", maxExpandedIPs)
	}
	out := make([]string, 0, end-start+1)
	for v := start; v <= end; v++ {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		out = append(out, net.IPv4(b[0], b[1], b[2], b[3]).String())
	}
	return out, nil
}

func parseIPv4(s string) (uint32, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, errors.New("not an IP")
	}
	v4 := ip.To4()
	if v4 == nil {
		return 0, errors.New("not an IPv4 address")
	}
	return binary.BigEndian.Uint32(v4), nil
}

func (h *Handler) requireAdminSession(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return nil, fleeterror.NewForbiddenError("only admins can manage fleet nodes")
	}
	return info, nil
}

// AWAITING_CONFIRMATION lives only on pending_enrollment, so a PENDING fleet
// node whose pending row is AWAITING_CONFIRMATION surfaces as such instead.
func deriveDisplayStatus(n fleetnodeenrollment.FleetNodeListing) pb.FleetNodeEnrollmentStatus {
	switch n.EnrollmentStatus {
	case fleetnodeenrollment.FleetNodeStatusPending:
		if n.PendingEnrollmentStatus == fleetnodeenrollment.StatusAwaitingConfirmation {
			return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_AWAITING_CONFIRMATION
		}
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_PENDING
	case fleetnodeenrollment.FleetNodeStatusConfirmed:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_CONFIRMED
	case fleetnodeenrollment.FleetNodeStatusRevoked:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_REVOKED
	}
	return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_UNSPECIFIED
}
