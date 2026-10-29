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

func TestRunOnNodeStopsPartitionsWhenNodeAcknowledgesRejectedReport(t *testing.T) {
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
		t.Fatal("request dispatched another partition after a rejected report and OK ack")
	default:
	}
}

func TestRunOnNodeLimitsReportsToThePartitionProbeCount(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	stream := registerConfiguredNode(reg, 7)
	defer stream.Unregister()
	admitted := make(chan [2]error, 1)
	go func() {
		cmd := <-stream.Outgoing
		var results [2]error
		results[0] = reg.AdmitReport(7, cmd.GetCommandId(), 5, control.ReportKindDiscovery)
		results[1] = reg.AdmitReport(7, cmd.GetCommandId(), 4, control.ReportKindDiscovery)
		admitted <- results
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
	}()

	// Act: 3 addresses x 2 ports = 6 probes
	err := NewService(reg, nil).RunOnNode(t.Context(), 7, "Fleet Node", ipRangeReq("10.0.0.1", "10.0.0.3", []string{"4028", "80"}), func(*pairingpb.DiscoverResponse) error { return nil })

	// Assert
	require.NoError(t, err)
	results := <-admitted
	require.NoError(t, results[0])
	require.ErrorIs(t, results[1], control.ErrReportQuotaExceeded)
}

func TestRunOnNodeDeadlineStopsNewPartitionsWithoutAbandoningInFlightCommand(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Arrange
		reg := control.NewRegistry()
		stream := registerConfiguredNode(reg, 7)
		defer stream.Unregister()
		commands := make(chan struct{}, 10)
		finished := make(chan struct{})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			defer close(finished)
			for {
				select {
				case cmd := <-stream.Outgoing:
					commands <- struct{}{}
					// Each partition fits in the per-command timeout; the second
					// is still scanning when the request deadline passes.
					select {
					case <-time.After(7 * time.Minute):
					case <-ctx.Done():
						return
					}
					reg.PublishBatch(7, cmd.GetCommandId(), &pairingpb.DiscoverResponse{Devices: []*pairingpb.Device{{DeviceIdentifier: "late", IpAddress: "10.0.0.5", Port: "4028"}}})
					stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
				case <-ctx.Done():
					return
				}
			}
		}()
		start := time.Now()
		var responses []*pairingpb.DiscoverResponse

		// Act
		err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node", ipRangeReq("10.0.0.1", "10.0.1.254", nil), collectResponses(&responses))
		elapsed := time.Since(start)
		cancel()
		<-finished

		// Assert
		require.NoError(t, err)
		require.Equal(t, 14*time.Minute, elapsed, "the in-flight command finishes after the 12m request deadline")
		require.Len(t, commands, 2, "no partition dispatches after the deadline")
		require.Len(t, responses, 3)
		require.Equal(t, "late", responses[1].GetDevices()[0].GetDeviceIdentifier())
		require.Contains(t, responses[2].GetWarning(), discoverylimits.RequestTimeoutWarning)
	})
}

func TestRunOnNodeInFlightCommandPastDeadlineStillHasItsOwnTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Arrange
		reg := control.NewRegistry()
		stream := registerConfiguredNode(reg, 7)
		defer stream.Unregister()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			first := true
			for {
				select {
				case cmd := <-stream.Outgoing:
					if !first {
						continue // the second command never acknowledges
					}
					first = false
					select {
					case <-time.After(7 * time.Minute):
					case <-ctx.Done():
						return
					}
					stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
				case <-ctx.Done():
					return
				}
			}
		}()
		start := time.Now()
		var responses []*pairingpb.DiscoverResponse

		// Act
		err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node", ipRangeReq("10.0.0.1", "10.0.1.254", nil), collectResponses(&responses))
		elapsed := time.Since(start)
		cancel()
		<-finished

		// Assert
		require.NoError(t, err)
		require.Equal(t, 7*time.Minute+DiscoverCommandTimeout, elapsed)
		require.Len(t, responses, 1)
		require.Contains(t, responses[0].GetWarning(), discoverylimits.RequestTimeoutWarning)
	})
}
