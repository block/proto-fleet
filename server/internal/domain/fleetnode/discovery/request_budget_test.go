package discovery

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestRunOnNodeExhaustedRequestDoesNotDispatch(t *testing.T) {
	// Arrange
	ctx, cancel := NewRequestContext(t.Context())
	defer cancel()
	require.True(t, discoverylimits.BudgetFromContext(ctx).Admit(discoverylimits.MaxReportsPerRequest))
	reg := control.NewRegistry()
	stream := registerConfiguredNode(reg, 7)
	defer stream.Unregister()
	var responses []*pairingpb.DiscoverResponse

	// Act
	err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node", ipRangeReq("10.0.0.1", "10.0.1.254", nil), collectResponses(&responses))

	// Assert
	require.NoError(t, err)
	require.Len(t, responses, 1)
	require.Contains(t, responses[0].GetWarning(), discoverylimits.ReportLimitWarning)
	select {
	case <-stream.Outgoing:
		t.Fatal("exhausted request dispatched a command")
	default:
	}
}

func TestRunOnNodeWarnsWhenNodeAcknowledgesRejectedReport(t *testing.T) {
	// Arrange
	ctx, cancel := NewRequestContext(t.Context())
	defer cancel()
	require.True(t, discoverylimits.BudgetFromContext(ctx).Admit(discoverylimits.MaxReportsPerRequest-1))
	reg := control.NewRegistry()
	stream := registerConfiguredNode(reg, 7)
	defer stream.Unregister()
	reported := make(chan error, 2)
	go func() {
		cmd := <-stream.Outgoing
		reported <- reg.AdmitReport(7, cmd.GetCommandId(), 1, control.ReportKindDiscovery)
		reg.PublishBatch(7, cmd.GetCommandId(), &pairingpb.DiscoverResponse{Devices: []*pairingpb.Device{{DeviceIdentifier: "accepted", IpAddress: "10.0.0.1", Port: "4028"}}})
		reported <- reg.AdmitReport(7, cmd.GetCommandId(), 1, control.ReportKindDiscovery)
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
	}()
	var responses []*pairingpb.DiscoverResponse

	// Act
	err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node", ipRangeReq("10.0.0.1", "10.0.1.254", nil), collectResponses(&responses))

	// Assert
	require.NoError(t, err)
	require.NoError(t, <-reported)
	require.ErrorIs(t, <-reported, control.ErrReportQuotaExceeded)
	require.Len(t, responses, 2)
	require.Equal(t, "accepted", responses[0].GetDevices()[0].GetDeviceIdentifier())
	require.Contains(t, responses[1].GetWarning(), discoverylimits.ReportLimitWarning)
	select {
	case <-stream.Outgoing:
		t.Fatal("request dispatched another command after a rejected report and OK ack")
	default:
	}
}

func TestRunOnNodeHonorsEarlierCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := control.NewRegistry()
		stream := registerConfiguredNode(reg, 7)
		defer stream.Unregister()
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		received := make(chan string, 1)
		go func() { received <- (<-stream.Outgoing).GetCommandId() }()
		var responses []*pairingpb.DiscoverResponse
		start := time.Now()

		err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node", ipListReq([]string{"10.0.0.1"}, []string{"4028"}), collectResponses(&responses))

		require.NoError(t, err)
		require.Equal(t, time.Minute, time.Since(start))
		require.Len(t, responses, 1)
		require.Contains(t, responses[0].GetWarning(), discoverylimits.RequestTimeoutWarning)
		_, active := reg.ReportScopeFor(7, <-received)
		require.False(t, active, "caller deadline must release the command session")
	})
}
