package fleetnodegateway

import (
	"context"
	"errors"
	"io"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeauth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodecontrol"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodepairing"
)

type Handler struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	enrollment *fleetnodeenrollment.Service
	auth       *fleetnodeauth.Service
	pairing    *fleetnodepairing.Service
	registry   *fleetnodecontrol.Registry
}

var _ fleetnodegatewayv1connect.FleetNodeGatewayServiceHandler = &Handler{}

func NewHandler(enrollment *fleetnodeenrollment.Service, auth *fleetnodeauth.Service, pairing *fleetnodepairing.Service, registry *fleetnodecontrol.Registry) *Handler {
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
		IdentityFingerprint: fleetnodeenrollment.IdentityFingerprint(agent.IdentityPubkey),
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
	subject, err := fleetnodeauth.GetSubject(ctx)
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
	subject, err := fleetnodeauth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	in := req.Msg.GetDevices()
	reports := make([]fleetnodepairing.DiscoveredDeviceReport, 0, len(in))
	for _, d := range in {
		reports = append(reports, fleetnodepairing.DiscoveredDeviceReport{
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
	accepted, _, err := h.pairing.UpsertDiscoveredDevices(ctx, subject.FleetNodeID, subject.OrgID, reports)
	if err != nil {
		return nil, err
	}
	// Forward this batch to the operator stream waiting on the
	// matching DiscoverOnFleetNode command. Persisted rows above are
	// the source of truth; this just wakes the operator UI.
	commandID := req.Msg.GetCommandId()
	if h.registry != nil && commandID != "" && accepted > 0 {
		batch := &pairingpb.DiscoverResponse{Devices: make([]*pairingpb.Device, 0, len(in))}
		for _, d := range in {
			batch.Devices = append(batch.Devices, &pairingpb.Device{
				DeviceIdentifier: d.GetDeviceIdentifier(),
				IpAddress:        d.GetIpAddress(),
				Port:             d.GetPort(),
				UrlScheme:        d.GetUrlScheme(),
				DriverName:       d.GetDriverName(),
				Model:            d.GetModel(),
				Manufacturer:     d.GetManufacturer(),
				FirmwareVersion:  d.GetFirmwareVersion(),
			})
		}
		h.registry.PublishBatch(subject.FleetNodeID, commandID, batch)
	}
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{
		AcceptedCount: accepted,
	}), nil
}

// ControlStream is the bidi RPC the agent opens after enrollment is
// CONFIRMED. The agent sends a Hello, the server sends an Accepted,
// then the server pushes ControlCommand messages and the agent
// replies with ControlAcks. The handler routes Acks to the
// fleetnodecontrol.Registry, which the admin RPC DiscoverOnFleetNode
// reads from to forward results to the operator.
func (h *Handler) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	subject, err := fleetnodeauth.GetSubject(ctx)
	if err != nil {
		return err
	}
	if h.registry == nil {
		return fleeterror.NewInternalErrorf("control stream registry not configured")
	}

	// First message must be Hello.
	first, err := stream.Receive()
	if err != nil {
		return fleeterror.NewInvalidArgumentErrorf("control stream closed before hello: %v", err)
	}
	if first.GetHello() == nil {
		return fleeterror.NewInvalidArgumentError("first ControlStreamRequest must be Hello")
	}

	regHandle, regErr := h.registry.Register(subject.FleetNodeID)
	if regErr != nil {
		return regErr
	}
	defer regHandle.Unregister()

	if sendErr := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{
		Accepted: &pb.ControlAccepted{ServerTime: timestamppb.New(time.Now().UTC())},
	}}); sendErr != nil {
		return fleeterror.NewInternalErrorf("send accepted: %v", sendErr)
	}

	// Pump incoming Acks on a side goroutine so the main loop can
	// also dispatch outgoing commands without blocking on Receive.
	type recvResult struct {
		msg *pb.ControlStreamRequest
		err error
	}
	incoming := make(chan recvResult, 1)
	go func() {
		for {
			msg, err := stream.Receive()
			incoming <- recvResult{msg: msg, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd, ok := <-regHandle.Outgoing:
			if !ok {
				return nil
			}
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
			// Stray Hellos after the first are ignored.
		}
	}
}

// MarshalDiscoverRequest is a thin helper exposed for the admin
// handler so it can encode the operator's pairing.v1.DiscoverRequest
// into ControlCommand.payload without depending on protobuf import
// gymnastics at the call site.
func MarshalDiscoverRequest(req *pairingpb.DiscoverRequest) ([]byte, error) {
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("marshal discover request: %v", err)
	}
	return b, nil
}
