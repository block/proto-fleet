package admin

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

// discoverAckFailure must translate each structured AckCode into a distinct,
// operator-meaningful gRPC code so a retryable BUSY and a capability gap
// (AGENT_INCAPABLE) don't both surface as an opaque Internal error.
func TestDiscoverAckFailure_MapsCodes(t *testing.T) {
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
			// Act
			err := discoverAckFailure(tc.ack)

			// Assert
			var fe fleeterror.FleetError
			require.ErrorAs(t, err, &fe)
			assert.Equal(t, tc.wantCode, fe.ConnectError().Code())
		})
	}
}
