package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/domain/nmaptarget"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment *enrollment.Service
	pairing    *pairing.Service
	registry   *control.Registry
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(enrollment *enrollment.Service, pairing *pairing.Service, registry *control.Registry) *Handler {
	return &Handler{enrollment: enrollment, pairing: pairing, registry: registry}
}

func (h *Handler) CreateEnrollmentCode(ctx context.Context, _ *connect.Request[pb.CreateEnrollmentCodeRequest]) (*connect.Response[pb.CreateEnrollmentCodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
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
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeRead, authz.ResourceContext{})
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
			IdentityFingerprint: enrollment.IdentityFingerprint(n.IdentityPubkey),
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
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
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
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.enrollment.RevokeFleetNode(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeFleetNodeResponse{}), nil
}

func (h *Handler) PairDeviceToFleetNode(ctx context.Context, req *connect.Request[pb.PairDeviceToFleetNodeRequest]) (*connect.Response[pb.PairDeviceToFleetNodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
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
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.pairing.UnpairDevice(ctx, req.Msg.GetDeviceId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UnpairDeviceResponse{}), nil
}

func (h *Handler) ListFleetNodeDevices(ctx context.Context, req *connect.Request[pb.ListFleetNodeDevicesRequest]) (*connect.Response[pb.ListFleetNodeDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var pairs []pairing.FleetNodeDevice
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

// DiscoverCommandTimeout bounds how long DiscoverOnFleetNode waits for the
// agent's batches and ack, so a silent node can't pin operator streams and
// registry slots. Must exceed the agent's scan budget (commandTimeout, 10m)
// plus report/ack slack: too short frees the slot mid-scan, the agent's ack is
// rejected as stale, and a new command dispatches while the node is still busy.
// Var for tests.
var DiscoverCommandTimeout = 12 * time.Minute

func (h *Handler) DiscoverOnFleetNode(ctx context.Context, req *connect.Request[pb.DiscoverOnFleetNodeRequest], stream *connect.ServerStream[pb.DiscoverOnFleetNodeResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
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
	if node.EnrollmentStatus != enrollment.FleetNodeStatusConfirmed {
		return fleeterror.NewFailedPreconditionError("fleet node is not CONFIRMED")
	}

	normalized, err := normalizeDiscoverRequest(discoverReq)
	if err != nil {
		return err
	}

	commandID := id.GenerateID()
	payload, err := proto.Marshal(normalized)
	if err != nil {
		return fleeterror.NewInternalErrorf("marshal discover payload: %v", err)
	}

	ctx, cancel := context.WithTimeout(ctx, DiscoverCommandTimeout)
	defer cancel()

	session, err := h.registry.Send(ctx, fleetNodeID, &gatewaypb.ControlCommand{
		CommandId: commandID,
		Payload:   payload,
	}, buildReportScope(normalized))
	if err != nil {
		if errors.Is(err, control.ErrNoActiveStream) {
			return fleeterror.NewFailedPreconditionError("fleet node has no active control stream")
		}
		return err
	}
	defer session.Close()

	// handleEvent forwards a batch or resolves the command on an ack. terminal
	// reports whether the command is finished; err is set only on failure.
	handleEvent := func(ev control.CommandEvent) (terminal bool, err error) {
		switch {
		case ev.Batch != nil:
			if sendErr := stream.Send(&pb.DiscoverOnFleetNodeResponse{Response: ev.Batch}); sendErr != nil {
				return true, fleeterror.NewInternalErrorf("send batch to operator: %v", sendErr)
			}
			return false, nil
		case ev.Ack != nil:
			// PARTIAL carries succeeded=false but its reports already streamed;
			// treat it as a usable (incomplete) result, not a failure.
			if ev.Ack.GetCode() == gatewaypb.AckCode_ACK_CODE_PARTIAL {
				slog.Warn("fleet node discovery completed partially",
					"fleet_node_id", fleetNodeID, "detail", ev.Ack.GetErrorMessage())
				return true, nil
			}
			// Require the structured OK code, not just the boolean, so an
			// inconsistent ack (succeeded=true with a non-OK/unset code) can't
			// pass a failed scan off as success.
			if ev.Ack.GetCode() != gatewaypb.AckCode_ACK_CODE_OK || !ev.Ack.GetSucceeded() {
				return true, discoverAckFailure(ev.Ack)
			}
			return true, nil
		default:
			return false, nil
		}
	}

	events := session.Events()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf("discovery command timed out after %s", DiscoverCommandTimeout))
			}
			return fleeterror.NewInternalErrorf("operator stream cancelled: %v", ctx.Err())
		case ev := <-events:
			if terminal, err := handleEvent(ev); terminal {
				return err
			}
		case <-session.Done():
			// Stream died before an ack. Drain buffered events first (a final
			// ack or last batch) so select randomness doesn't drop them.
			for {
				select {
				case ev := <-events:
					if terminal, err := handleEvent(ev); terminal {
						return err
					}
				default:
					return fleeterror.NewFailedPreconditionError("fleet node control stream closed before command completed")
				}
			}
		}
	}
}

// discoverAckFailure maps a non-OK ack to an operator-facing error, even when
// error_message is empty. The structured AckCode drives the gRPC code so the
// operator can tell a retryable condition (BUSY) and a capability gap
// (AGENT_INCAPABLE) apart from a malformed request (BAD_REQUEST); anything else
// is an opaque Internal failure.
func discoverAckFailure(ack *gatewaypb.ControlAck) error {
	reason := ack.GetErrorMessage()
	if reason == "" {
		reason = "code " + ack.GetCode().String()
	}
	// if/else (not switch) so the exhaustive linter doesn't demand a case per
	// AckCode; everything outside these three is an opaque Internal failure.
	code := ack.GetCode()
	if code == gatewaypb.AckCode_ACK_CODE_BAD_REQUEST {
		return fleeterror.NewInvalidArgumentErrorf("fleet node rejected discovery command: %s", reason)
	}
	if code == gatewaypb.AckCode_ACK_CODE_BUSY {
		return fleeterror.NewPlainError(
			fmt.Sprintf("fleet node is busy with another command; retry shortly: %s", reason),
			connect.CodeResourceExhausted,
		)
	}
	if code == gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE {
		return fleeterror.NewFailedPreconditionErrorf("fleet node cannot service this discovery request; try another node: %s", reason)
	}
	return fleeterror.NewInternalErrorf("fleet node reported discovery failure: %s", reason)
}

func normalizeDiscoverRequest(in *pairingpb.DiscoverRequest) (*pairingpb.DiscoverRequest, error) {
	switch m := in.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		if m.IpList == nil || len(m.IpList.GetIpAddresses()) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("ip_list.ip_addresses must not be empty")
		}
		if err := checkScanLimits(m.IpList.GetIpAddresses(), m.IpList.GetPorts()); err != nil {
			return nil, err
		}
		// Every entry must be a valid IP or hostname, and IP literals must be
		// private. A malformed token (e.g. "bad/entry") is unresolvable for the
		// agent yet trips the scope matcher's hostname fallback, widening the
		// command to port-only scope. A public literal scans fine but every report
		// is rejected by validateReport (private-only), surfacing as a late
		// REPORT_FAILED. Hostnames resolve agent-side to an IP the server can't
		// check here, so they pass through.
		for _, e := range m.IpList.GetIpAddresses() {
			addr, perr := netip.ParseAddr(e)
			if perr != nil {
				if !nmaptarget.IsHostname(e) {
					return nil, fleeterror.NewInvalidArgumentErrorf("ip_list entry %q is not a valid IP address or hostname", e)
				}
				continue
			}
			if !addr.Unmap().IsPrivate() {
				return nil, fleeterror.NewInvalidArgumentErrorf("ip_list entry %q is not a private (RFC1918/RFC4193) address", e)
			}
		}
		return in, nil
	case *pairingpb.DiscoverRequest_IpRange:
		ips, err := expandIPv4Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp())
		if err != nil {
			return nil, err
		}
		if err := checkScanLimits(ips, m.IpRange.GetPorts()); err != nil {
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
		target := m.Nmap.GetTarget()
		// Validate against the shared grammar (incl. the /22 CIDR cap), then
		// reject IPv6 CIDR — both rejections the agent makes — so an unsupported
		// target fails fast here instead of as a late agent BAD_REQUEST ack.
		if err := nmaptarget.Validate(target); err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		if prefix, perr := netip.ParsePrefix(target); perr == nil && prefix.Addr().Is6() {
			return nil, fleeterror.NewInvalidArgumentError("nmap IPv6 CIDR is not supported; use ip_list for IPv6 devices")
		}
		// A public target scans fine but every report comes back non-private and
		// is rejected by validateReport, so fail fast. Hostnames resolve agent-side
		// and pass through (the report validator still guards what they return).
		if !nmapTargetIsPrivate(target) {
			return nil, fleeterror.NewInvalidArgumentError("nmap target must be within a private (RFC1918/RFC4193) range")
		}
		if err := checkScanLimits(nil, m.Nmap.GetPorts()); err != nil {
			return nil, err
		}
		return in, nil
	case *pairingpb.DiscoverRequest_Mdns:
		return nil, fleeterror.NewInvalidArgumentError("mdns discovery is not supported on fleet nodes")
	default:
		return nil, fleeterror.NewInvalidArgumentError("discover request mode is required")
	}
}

// checkScanLimits enforces the agent's per-command caps (via discoverylimits)
// and rejects malformed ports before dispatch, so an over-cap or invalid request
// fails fast with a validation error instead of a late agent BAD_REQUEST ack.
// The proto caps are the wire ceiling; these are the real limits.
func checkScanLimits(ipAddresses, ports []string) error {
	if len(ipAddresses) > discoverylimits.MaxScanTargets {
		return fleeterror.NewInvalidArgumentErrorf("too many targets: %d exceeds the limit of %d", len(ipAddresses), discoverylimits.MaxScanTargets)
	}
	if len(ports) > discoverylimits.MaxPortsPerIP {
		return fleeterror.NewInvalidArgumentErrorf("too many ports: %d exceeds the limit of %d", len(ports), discoverylimits.MaxPortsPerIP)
	}
	// Each port must be a bare decimal in 1-65535, matching the agent's
	// resolveAndValidatePorts; otherwise a token like "80/tcp" or "70000"
	// dispatches and returns as a late agent BAD_REQUEST ack.
	for _, p := range ports {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return fleeterror.NewInvalidArgumentErrorf("invalid port %q: must be a decimal in 1-65535", p)
		}
	}
	return nil
}

func expandIPv4Range(startStr, endStr string) ([]string, error) {
	startAddr, err := netutil.ParseIPv4(startStr)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid start_ip: %v", err)
	}
	endAddr, err := netutil.ParseIPv4(endStr)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid end_ip: %v", err)
	}
	// Both ends must be private. The MaxScanTargets cap below keeps the range far
	// smaller than the gap between RFC1918 blocks, so private endpoints imply a
	// fully private range. A public range scans fine but every report is rejected
	// by validateReport, surfacing as a late REPORT_FAILED.
	if !startAddr.IsPrivate() || !endAddr.IsPrivate() {
		return nil, fleeterror.NewInvalidArgumentError("ip range must be within a private (RFC1918) range")
	}
	start, end := netutil.IPv4ToUint32(startAddr), netutil.IPv4ToUint32(endAddr)
	if end < start {
		return nil, fleeterror.NewInvalidArgumentError("end_ip must be >= start_ip")
	}
	// Skip the network (.0) and gateway (.1) start addresses, matching the agent
	// and cloud pairing. Otherwise expanding to an IP list would scan .0/.1 as
	// literal targets — gateways answer on many ports and look like miners.
	start = netutil.AdjustIPv4RangeStart(start)
	if end < start {
		return nil, fleeterror.NewInvalidArgumentError("ip range covers only network/gateway addresses")
	}
	// uint64 math so a range ending at 255.255.255.255 can't wrap (in uint32,
	// end-start+1 would overflow to 0, bypassing the cap and never terminating).
	size := uint64(end) - uint64(start) + 1
	if size > discoverylimits.MaxScanTargets {
		return nil, fleeterror.NewInvalidArgumentErrorf("ip range exceeds %d addresses", discoverylimits.MaxScanTargets)
	}
	out := make([]string, 0, size)
	for v := start; ; v++ {
		out = append(out, netutil.Uint32ToIPv4(v))
		if v == end {
			break
		}
	}
	return out, nil
}

// AWAITING_CONFIRMATION lives only on pending_enrollment, so a PENDING fleet
// node whose pending row is AWAITING_CONFIRMATION surfaces as such instead.
func deriveDisplayStatus(n enrollment.FleetNodeListing) pb.FleetNodeEnrollmentStatus {
	switch n.EnrollmentStatus {
	case enrollment.FleetNodeStatusPending:
		if n.PendingEnrollmentStatus == enrollment.StatusAwaitingConfirmation {
			return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_AWAITING_CONFIRMATION
		}
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_PENDING
	case enrollment.FleetNodeStatusConfirmed:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_CONFIRMED
	case enrollment.FleetNodeStatusRevoked:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_REVOKED
	}
	return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_UNSPECIFIED
}
