package control

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// AckFailure must translate each structured AckCode into a distinct,
// operator-meaningful gRPC code so a retryable BUSY and a capability gap
// (AGENT_INCAPABLE) don't both surface as an opaque Internal error.
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
