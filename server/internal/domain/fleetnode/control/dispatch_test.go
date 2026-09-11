package control

import (
	"errors"
	"fmt"
	"testing"
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
			name:     "unauthenticated stays an authentication error",
			ack:      &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED},
			wantCode: connect.CodeUnauthenticated,
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
