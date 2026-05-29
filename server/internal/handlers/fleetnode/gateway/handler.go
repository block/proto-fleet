package gateway

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
)

type Handler struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	enrollment *enrollment.Service
	auth       *auth.Service
	pairing    *pairing.Service
	registry   *control.Registry
}

var _ fleetnodegatewayv1connect.FleetNodeGatewayServiceHandler = &Handler{}

func NewHandler(enrollment *enrollment.Service, auth *auth.Service, pairing *pairing.Service, registry *control.Registry) *Handler {
	return &Handler{enrollment: enrollment, auth: auth, pairing: pairing, registry: registry}
}

func (h *Handler) Register(ctx context.Context, req *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
	agent, _, err := h.enrollment.RegisterFleetNode(ctx, req.Msg.GetEnrollmentToken(), req.Msg.GetName(), req.Msg.GetIdentityPubkey(), req.Msg.GetMinerSigningPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RegisterResponse{
		FleetNodeId:         agent.ID,
		EnrollmentStatus:    pb.EnrollmentStatus_ENROLLMENT_STATUS_PENDING,
		IdentityFingerprint: enrollment.IdentityFingerprint(agent.IdentityPubkey),
	}), nil
}

func (h *Handler) BeginAuthHandshake(ctx context.Context, req *connect.Request[pb.BeginAuthHandshakeRequest]) (*connect.Response[pb.BeginAuthHandshakeResponse], error) {
	challenge, expiresAt, err := h.auth.BeginHandshake(ctx, req.Msg.GetApiKey(), req.Msg.GetIdentityPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BeginAuthHandshakeResponse{
		Challenge: challenge,
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) CompleteAuthHandshake(ctx context.Context, req *connect.Request[pb.CompleteAuthHandshakeRequest]) (*connect.Response[pb.CompleteAuthHandshakeResponse], error) {
	token, expiresAt, err := h.auth.CompleteHandshake(ctx, req.Msg.GetChallenge(), req.Msg.GetSignature())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CompleteAuthHandshakeResponse{
		SessionToken: token,
		ExpiresAt:    timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) UploadHeartbeat(ctx context.Context, _ *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := h.enrollment.UpdateLastSeen(ctx, subject.FleetNodeID, subject.OrgID, now); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UploadHeartbeatResponse{
		ReceivedAt: timestamppb.New(now),
	}), nil
}

func (h *Handler) ReportDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error) {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	commandID := req.Msg.GetCommandId()
	if commandID == "" {
		return nil, fleeterror.NewFailedPreconditionError("discovery report requires a command_id from a server-issued ControlCommand")
	}
	in := req.Msg.GetDevices()
	// Bind to the in-flight command and reserve quota so an agent can't stream
	// unbounded batches against one command_id.
	if admitErr := h.registry.AdmitReport(subject.FleetNodeID, commandID, len(in)); admitErr != nil {
		if errors.Is(admitErr, control.ErrReportQuotaExceeded) {
			return nil, connect.NewError(connect.CodeResourceExhausted, admitErr)
		}
		return nil, fleeterror.NewFailedPreconditionError("discovery report does not match an in-flight server-issued command")
	}

	// Drop devices outside the command's requested scan scope so a compromised
	// node can't report (or claim) devices it was never asked to scan. A nil
	// scope is unconstrained; ok is false only if the command was torn down
	// between AdmitReport and here (then nothing is in scope).
	scope, ok := h.registry.ReportScopeFor(subject.FleetNodeID, commandID)
	inScope := make([]*pb.DiscoveredDeviceReport, 0, len(in))
	var outOfScope int64
	for _, d := range in {
		if ok && (scope == nil || scope(d.GetIpAddress(), d.GetPort())) {
			inScope = append(inScope, d)
		} else {
			outOfScope++
		}
	}

	reports := make([]pairing.DiscoveredDeviceReport, 0, len(inScope))
	for _, d := range inScope {
		reports = append(reports, pairing.DiscoveredDeviceReport{
			DeviceIdentifier: d.GetDeviceIdentifier(),
			IPAddress:        d.GetIpAddress(),
			Port:             d.GetPort(),
			URLScheme:        d.GetUrlScheme(),
			DriverName:       d.GetDriverName(),
			Model:            d.GetModel(),
			Manufacturer:     d.GetManufacturer(),
			FirmwareVersion:  d.GetFirmwareVersion(),
		})
	}
	acceptedIdx, ownershipRejected, err := h.pairing.UpsertDiscoveredDevices(ctx, subject.FleetNodeID, subject.OrgID, reports)
	if err != nil {
		return nil, err
	}
	if outOfScope > 0 || ownershipRejected > 0 {
		slog.Warn("fleet node reported devices that were dropped",
			"fleet_node_id", subject.FleetNodeID,
			"org_id", subject.OrgID,
			"out_of_scope", outOfScope,
			"ownership_rejected", ownershipRejected,
		)
	}
	if len(acceptedIdx) > 0 {
		// Forward only store-accepted devices; out-of-scope and
		// ownership/attribution-rejected rows must not surface to the operator.
		batch := &pairingpb.DiscoverResponse{Devices: make([]*pairingpb.Device, 0, len(acceptedIdx))}
		for _, i := range acceptedIdx {
			batch.Devices = append(batch.Devices, toPairingDevice(inScope[i]))
		}
		h.registry.PublishBatch(subject.FleetNodeID, commandID, batch)
	}
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{
		AcceptedCount: int64(len(acceptedIdx)),
		RejectedCount: ownershipRejected + outOfScope,
	}), nil
}

func toPairingDevice(d *pb.DiscoveredDeviceReport) *pairingpb.Device {
	return &pairingpb.Device{
		DeviceIdentifier: d.GetDeviceIdentifier(),
		IpAddress:        d.GetIpAddress(),
		Port:             d.GetPort(),
		UrlScheme:        d.GetUrlScheme(),
		DriverName:       d.GetDriverName(),
		Model:            d.GetModel(),
		Manufacturer:     d.GetManufacturer(),
		FirmwareVersion:  d.GetFirmwareVersion(),
	}
}

// HelloTimeout bounds the wait for the agent's first Hello, so a node that
// opens the stream and never sends one can't pin a goroutine + HTTP/2 stream
// indefinitely. Var so tests can shrink it.
var HelloTimeout = 5 * time.Second

func (h *Handler) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	subject, err := auth.GetSubject(ctx)
	if err != nil {
		return err
	}

	// streamMsg carries a blocking stream.Receive() result out of the reader
	// goroutine so the selects below can multiplex it against timeouts/commands.
	type streamMsg struct {
		msg *pb.ControlStreamRequest
		err error
	}
	helloCh := make(chan streamMsg, 1)
	go func() {
		msg, err := stream.Receive()
		helloCh <- streamMsg{msg: msg, err: err}
	}()

	// NewTimer + Stop (not time.After) releases the timer once Hello arrives,
	// instead of lingering until HelloTimeout on every successful connection.
	helloTimer := time.NewTimer(HelloTimeout)
	defer helloTimer.Stop()
	var first *pb.ControlStreamRequest
	select {
	case <-helloTimer.C:
		return fleeterror.NewFailedPreconditionErrorf("control stream Hello not received within %s", HelloTimeout)
	case <-ctx.Done():
		return fleeterror.NewInternalErrorf("control stream closed before hello: %v", ctx.Err())
	case r := <-helloCh:
		if r.err != nil {
			return fleeterror.NewInvalidArgumentErrorf("control stream closed before hello: %v", r.err)
		}
		first = r.msg
	}
	if first.GetHello() == nil {
		return fleeterror.NewInvalidArgumentError("first ControlStreamRequest must be Hello")
	}

	regHandle := h.registry.Register(subject.FleetNodeID)
	defer regHandle.Unregister()

	if sendErr := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{
		Accepted: &pb.ControlAccepted{ServerTime: timestamppb.New(time.Now().UTC())},
	}}); sendErr != nil {
		return fleeterror.NewInternalErrorf("send accepted: %v", sendErr)
	}

	// Side-goroutine bridges blocking stream.Receive into the select loop. Its
	// send selects on regHandle.Done (closed by the deferred Unregister) so it
	// can't block forever on a full channel after the main loop exits.
	incoming := make(chan streamMsg, 2)
	go func() {
		for {
			msg, err := stream.Receive()
			select {
			case incoming <- streamMsg{msg: msg, err: err}:
			case <-regHandle.Done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-regHandle.Done:
			// Newest-wins eviction or Unregister fired; let the handler
			// exit so connect-go closes the stream.
			return nil
		case cmd := <-regHandle.Outgoing:
			if sendErr := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: cmd}}); sendErr != nil {
				return fleeterror.NewInternalErrorf("send command: %v", sendErr)
			}
		case r := <-incoming:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					return nil
				}
				return fleeterror.NewInternalErrorf("control stream recv: %v", r.err)
			}
			if ack := r.msg.GetAck(); ack != nil {
				regHandle.PublishAck(ack)
			}
		}
	}
}
