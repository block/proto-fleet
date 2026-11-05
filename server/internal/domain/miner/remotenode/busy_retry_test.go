package remotenode

import (
	"context"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestRetryRejectedBusyBoundsReacquireByMinRemaining(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	minRemaining := 500 * time.Millisecond
	sends := 0

	ack, err := RetryRejectedBusy(ctx, func() {}, minRemaining,
		func(acquireCtx context.Context) (func(), error) {
			got, ok := acquireCtx.Deadline()
			require.True(t, ok)
			require.Equal(t, deadline.Add(-minRemaining), got)
			<-acquireCtx.Done()
			return nil, acquireCtx.Err()
		},
		func(context.Context) (*gatewaypb.ControlAck, error) {
			sends++
			return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
		},
	)

	require.NoError(t, err)
	require.True(t, IsRejectedBusy(ack))
	require.Equal(t, 1, sends)
	require.NoError(t, ctx.Err(), "reacquire must give up before the command deadline")
}
