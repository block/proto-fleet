package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestRegistry_PreEnqueueCommandFailuresCarryEvidence(t *testing.T) {
	for _, artifacts := range []bool{false, true} {
		for _, failure := range []string{"missing stream", "protocol", "duplicate ID", "canceled before enqueue"} {
			t.Run(failure+map[bool]string{false: "/ordinary", true: "/artifacts"}[artifacts], func(t *testing.T) {
				r := NewRegistry()
				ctx := t.Context()
				if failure != "missing stream" {
					version := gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1
					if failure == "protocol" {
						version = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_UNSPECIFIED
					}
					stream, err := r.RegisterAuthenticated(7, "session", version)
					require.NoError(t, err)
					defer stream.Unregister()
					if failure == "duplicate ID" {
						session, err := r.Send(ctx, 7, version, &gatewaypb.ControlCommand{CommandId: "command"}, nil, ReportKindDiscovery, nil)
						require.NoError(t, err)
						defer session.Close()
						require.Equal(t, "command", recvCommandID(t, stream))
					}
					if failure == "canceled before enqueue" {
						canceled, cancel := context.WithCancel(ctx)
						cancel()
						ctx = canceled
					}
				}
				var err error
				cmd := &gatewaypb.ControlCommand{CommandId: "command"}
				if artifacts {
					_, _, err = r.SendCommandWithArtifactResults(ctx, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd, nil)
				} else {
					_, err = r.SendCommand(ctx, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd)
				}
				require.Error(t, err)
				require.True(t, IsCommandNotEnqueued(err))
				switch failure {
				case "missing stream":
					require.ErrorIs(t, err, ErrNoActiveStream)
				case "protocol":
					require.True(t, fleeterror.IsFailedPreconditionError(err))
					require.ErrorContains(t, err, "upgrade Fleet Node")
				case "duplicate ID":
					var internal fleeterror.FleetError
					require.True(t, errors.As(err, &internal))
					require.Equal(t, connect.CodeInternal, internal.GRPCCode)
					require.ErrorContains(t, err, errDuplicateCommandID.Error())
				case "canceled before enqueue":
					var internal fleeterror.FleetError
					require.True(t, errors.As(err, &internal))
					require.Equal(t, connect.CodeInternal, internal.GRPCCode)
					require.ErrorContains(t, err, "send command: context canceled")
				}
			})
		}
	}
	_, err := NewRegistry().Send(t.Context(), 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "report"}, nil, ReportKindDiscovery, nil)
	require.ErrorIs(t, err, ErrNoActiveStream)
	require.False(t, IsCommandNotEnqueued(err), "report-bearing Send retains its existing semantics")
	require.False(t, IsCommandNotEnqueued(ErrNoActiveStream), "the sentinel alone is not pre-enqueue evidence")
}

func TestRegistry_PostEnqueueCommandFailuresRemainUncertain(t *testing.T) {
	for _, artifacts := range []bool{false, true} {
		for _, failure := range []string{"disconnect", "deadline"} {
			t.Run(failure+map[bool]string{false: "/ordinary", true: "/artifacts"}[artifacts], func(t *testing.T) {
				r := NewRegistry()
				stream := r.Register(7)
				defer stream.Unregister()
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				result := make(chan cmdResult, 1)
				go func() {
					cmd := &gatewaypb.ControlCommand{CommandId: "command"}
					if artifacts {
						ack, _, err := r.SendCommandWithArtifactResults(ctx, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd, nil)
						result <- cmdResult{ack: ack, err: err}
					} else {
						ack, err := r.SendCommand(ctx, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd)
						result <- cmdResult{ack: ack, err: err}
					}
				}()
				require.Equal(t, "command", recvCommandID(t, stream), "enqueue succeeded before the failure")
				if failure == "disconnect" {
					stream.Unregister()
				} else {
					<-ctx.Done()
				}
				res := recvResult(t, result)
				require.Error(t, res.err)
				require.False(t, IsCommandNotEnqueued(res.err))
				if failure == "disconnect" {
					require.ErrorIs(t, res.err, ErrNoActiveStream)
				} else {
					require.ErrorContains(t, res.err, "await ack")
					require.ErrorIs(t, res.err, context.DeadlineExceeded)
				}
			})
		}
	}
}
