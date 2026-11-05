package remotenode

import (
	"context"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

var busyRetryBackoffs = [...]time.Duration{25 * time.Millisecond, 100 * time.Millisecond}

// busyRetryMinRemaining keeps a retry from turning a known pre-dispatch
// rejection into an ambiguous timeout when too little budget remains.
var busyRetryMinRemaining = time.Second

// IsRejectedBusy reports whether the node refused the command before dispatch.
func IsRejectedBusy(ack *gatewaypb.ControlAck) bool {
	return ack.GetCode() == gatewaypb.AckCode_ACK_CODE_BUSY && !ack.GetSucceeded()
}

// RetryRejectedBusy sends using the already-held permit, then retries explicit
// BUSY rejections with fresh permits within ctx's deadline. When a retry cannot
// start, it returns the last BUSY ack so callers still treat the outcome as
// capacity rather than a device failure.
func RetryRejectedBusy(
	ctx context.Context,
	release func(),
	minRemaining time.Duration,
	acquire func(context.Context) (func(), error),
	send func(context.Context) (*gatewaypb.ControlAck, error),
) (*gatewaypb.ControlAck, error) {
	ack, err := sendThenRelease(ctx, release, send)
	for _, backoff := range busyRetryBackoffs {
		if err != nil || !IsRejectedBusy(ack) || !waitForBusyRetry(ctx, backoff, minRemaining) {
			break
		}
		next, acquireErr := acquire(ctx)
		if acquireErr != nil {
			break
		}
		ack, err = sendThenRelease(ctx, next, send)
	}
	return ack, err
}

func waitForBusyRetry(ctx context.Context, backoff, minRemaining time.Duration) bool {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < backoff+minRemaining {
		return false
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func sendThenRelease(
	ctx context.Context,
	release func(),
	send func(context.Context) (*gatewaypb.ControlAck, error),
) (*gatewaypb.ControlAck, error) {
	defer release()
	return send(ctx)
}
