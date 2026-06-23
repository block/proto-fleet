package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/nmaptarget"
)

type stubLister struct {
	nodes []enrollment.FleetNodeListing
	err   error
}

func (s stubLister) ListFleetNodes(context.Context, int64) ([]enrollment.FleetNodeListing, error) {
	return s.nodes, s.err
}

func collectBatches(dst *[]*pairingpb.Device) func(*pairingpb.DiscoverResponse) error {
	return func(b *pairingpb.DiscoverResponse) error {
		*dst = append(*dst, b.GetDevices()...)
		return nil
	}
}

func TestRunOnNode_ForwardsBatchesUntilAck(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(7)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:1", IpAddress: "10.0.0.5", Port: "4028"}},
		})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	}()
	var got []*pairingpb.Device

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.5"}, []string{"4028"}), collectBatches(&got))

	// Assert
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "auto:1", got[0].GetDeviceIdentifier())
}

func TestRunOnNode_PartialAckIsNotFailure(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(8)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:2", IpAddress: "10.0.0.6", Port: "4028"}},
		})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	}()
	var got []*pairingpb.Device

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.6"}, []string{"4028"}), collectBatches(&got))

	// Assert: PARTIAL is a usable (incomplete) result, and its batch still streamed.
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestRunOnNode_DisconnectBeforeAckReturnsError(t *testing.T) {
	// Arrange: agent takes the command then drops without acking.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(9)
	stream := reg.Register(nodeID)
	go func() {
		<-stream.Outgoing
		stream.Unregister()
	}()

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.7"}, nil), func(*pairingpb.DiscoverResponse) error { return nil })

	// Assert
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeFailedPrecondition, fe.ConnectError().Code())
}

func TestRunOnNode_NoActiveStreamReturnsFailedPrecondition(t *testing.T) {
	// Arrange: no stream registered for the target node.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})

	// Act
	err := svc.RunOnNode(context.Background(), 404, ipListReq([]string{"10.0.0.8"}, nil), func(*pairingpb.DiscoverResponse) error { return nil })

	// Assert
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeFailedPrecondition, fe.ConnectError().Code())
}

func TestConfirmedConnectedNodeIDs_IntersectsStatusAndConnection(t *testing.T) {
	// Arrange: 1 = confirmed+connected, 2 = confirmed+disconnected, 3 = pending+connected.
	reg := control.NewRegistry()
	lister := stubLister{nodes: []enrollment.FleetNodeListing{
		{FleetNode: enrollment.FleetNode{ID: 1, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
		{FleetNode: enrollment.FleetNode{ID: 2, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
		{FleetNode: enrollment.FleetNode{ID: 3, EnrollmentStatus: enrollment.FleetNodeStatusPending}},
	}}
	svc := NewService(reg, lister)
	s1 := reg.Register(1)
	defer s1.Unregister()
	s3 := reg.Register(3)
	defer s3.Unregister()

	// Act
	got, err := svc.ConfirmedConnectedNodeIDs(context.Background(), 1)

	// Assert: only the confirmed AND connected node (order is unspecified).
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{1}, got)
}

func TestRunOnNode_OnBatchErrorIsTerminal(t *testing.T) {
	// Arrange: the agent emits a batch; the caller's onBatch reports its stream gone.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(11)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:x", IpAddress: "10.0.0.5", Port: "4028"}},
		})
	}()
	sentinel := errors.New("operator stream gone")

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.5"}, []string{"4028"}), func(*pairingpb.DiscoverResponse) error {
		return sentinel
	})

	// Assert: an onBatch error terminates RunOnNode with that error.
	require.ErrorIs(t, err, sentinel)
}

func TestRunOnNode_TimesOutWhenAgentNeverAcks(t *testing.T) {
	// Arrange: shrink the command timeout, drain the command, but never ack.
	prev := DiscoverCommandTimeout
	DiscoverCommandTimeout = 100 * time.Millisecond
	t.Cleanup(func() { DiscoverCommandTimeout = prev })
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(12)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	go func() { <-stream.Outgoing }()

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.5"}, nil), func(*pairingpb.DiscoverResponse) error { return nil })

	// Assert
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeDeadlineExceeded, ce.Code())
}

func TestConfirmedConnectedNodeIDs_PropagatesListError(t *testing.T) {
	// Arrange: the enrollment lookup fails.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{err: errors.New("db unavailable")})

	// Act
	_, err := svc.ConfirmedConnectedNodeIDs(context.Background(), 1)

	// Assert: the error propagates (fan-out treats it as best-effort upstream).
	require.Error(t, err)
}

func TestRunOnNode_DispatchesLocalSubnetTargetSentinel(t *testing.T) {
	// Arrange: the LocalSubnetTarget sentinel (operator single-node scan or fan-out)
	// must reach the agent unchanged so the node scans its own subnet.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(21)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	gotTarget := make(chan string, 1)
	go func() {
		cmd := <-stream.Outgoing
		var env gatewaypb.AgentCommand
		_ = proto.Unmarshal(cmd.GetPayload(), &env)
		gotTarget <- env.GetDiscover().GetNmap().GetTarget()
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	}()
	req := &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_Nmap{
		Nmap: &pairingpb.NmapModeRequest{Target: nmaptarget.LocalSubnetTarget},
	}}

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, req, func(*pairingpb.DiscoverResponse) error { return nil })

	// Assert
	require.NoError(t, err)
	assert.Equal(t, nmaptarget.LocalSubnetTarget, <-gotTarget)
}
