package control

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// AckFailure must translate each structured AckCode into a distinct,
// operator-meaningful gRPC code so a retryable BUSY and unsupported commands
// don't surface as opaque Internal errors.
func TestAckFailure_MapsCodes(t *testing.T) {
	tests := []struct {
		name     string
		ack      *gatewaypb.ControlAck
		wantCode connect.Code
	}{
		{
			name:     "bad request maps to invalid argument",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BAD_REQUEST},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "miner credentials map to failed precondition",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED},
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "busy maps to resource exhausted",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY},
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name:     "agent incapable maps to failed precondition",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE},
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "unimplemented maps to unimplemented",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED},
			wantCode: connect.CodeUnimplemented,
		},
		{
			name:     "unknown code falls back to internal",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNSPECIFIED},
			wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ack := tc.ack

			// Act
			err := AckFailure(ack, "discovery")

			// Assert
			var fe fleeterror.FleetError
			require.ErrorAs(t, err, &fe)
			assert.Equal(t, tc.wantCode, fe.ConnectError().Code())
		})
	}
}

func TestRunCommand_PartialAckReachesCallback(t *testing.T) {
	for _, callbackErr := range []error{nil, errors.New("operator disconnected")} {
		t.Run(fmt.Sprint(callbackErr), func(t *testing.T) {
			registry := NewRegistry()
			stream := registry.Register(7)
			defer stream.Unregister()
			go func() {
				cmd := <-stream.Outgoing
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL, ErrorMessage: "scan deadline"})
			}()
			var got []*gatewaypb.ControlAck
			err := RunCommand(t.Context(), registry, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
				&gatewaypb.ControlCommand{CommandId: "partial"}, nil, ReportKindDiscovery, nil, time.Second, "discovery",
				func(ev CommandEvent) (bool, error) { got = append(got, ev.Ack); return false, callbackErr })
			require.ErrorIs(t, err, callbackErr)
			require.Len(t, got, 1)
			assert.Equal(t, "scan deadline", got[0].GetErrorMessage())
		})
	}
}

func TestRunCommandToCompletionPreservesTimeoutBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := NewRegistry()
		stream := registry.Register(7)
		defer stream.Unregister()
		// Enqueue consumes six seconds; the caller expires one second later.
		// Completion must ignore that caller deadline but still end at 12 seconds,
		// not receive another full timeout after Send returns.
		ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
		defer cancel()
		start := time.Now()
		err := RunCommandToCompletion(ctx, delayedCommandSender{Sender: registry, delay: 6 * time.Second}, 7,
			gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &gatewaypb.ControlCommand{CommandId: "timeout"},
			nil, ReportKindPair, nil, 12*time.Second, "pair", func(CommandEvent) (bool, error) { return false, nil })

		require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
		assert.Equal(t, 12*time.Second, time.Since(start))
		require.Len(t, stream.Outgoing, 1, "command must have been accepted before caller deadline")
	})
}

func TestRunCommandToCompletionCancellationWhileEnqueueBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := NewRegistry()
		stream := registry.Register(7)
		defer stream.Unregister()
		// Fill the real outbound queue so the next Send cannot be accepted.
		for i := range outgoingBuffer {
			session, err := registry.Send(t.Context(), 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
				&gatewaypb.ControlCommand{CommandId: fmt.Sprint(i)}, nil, ReportKindDiscovery, nil)
			require.NoError(t, err)
			session.Close()
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- RunCommandToCompletion(ctx, registry, 7, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
				&gatewaypb.ControlCommand{CommandId: "blocked"}, nil, ReportKindPair, nil, time.Minute, "pair",
				func(CommandEvent) (bool, error) { return false, nil })
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			assert.True(t, fleeterror.IsCanceledError(err), "enqueue cancellation must remain a canceled error: %v", err)
		default:
			t.Fatal("cancellation did not interrupt blocked enqueue")
		}
		assert.Len(t, stream.Outgoing, outgoingBuffer)
	})
}

type delayedCommandSender struct {
	Sender
	delay time.Duration
}

func (s delayedCommandSender) Send(ctx context.Context, nodeID int64, minimumVersion gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand, scope ReportScope, kind ReportKind, pair *PairMeta) (*Session, error) {
	time.Sleep(s.delay)
	return s.Sender.Send(ctx, nodeID, minimumVersion, cmd, scope, kind, pair)
}
