package remotenode

import (
	"context"
	"log/slog"
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
// capacity rather than a device failure. logAttrs identify the command in the
// debug log emitted after any BUSY.
func RetryRejectedBusy(
	ctx context.Context,
	release func(),
	minRemaining time.Duration,
	acquire func(context.Context) (func(), error),
	send func(context.Context) (*gatewaypb.ControlAck, error),
	logAttrs ...any,
) (*gatewaypb.ControlAck, error) {
	ack, err := sendThenRelease(ctx, release, send)
	if err != nil || !IsRejectedBusy(ack) {
		return ack, err
	}
	sends := 1
	for _, backoff := range busyRetryBackoffs {
		if !waitForBusyRetry(ctx, backoff, minRemaining) {
			break
		}
		next, acquireErr := acquireLeavingBudget(ctx, minRemaining, acquire)
		if acquireErr != nil {
			break
		}
		ack, err = sendThenRelease(ctx, next, send)
		sends++
		if err != nil || !IsRejectedBusy(ack) {
			break
		}
	}
	if logger := slog.Default(); logger.Enabled(ctx, slog.LevelDebug) {
		logger.With(logAttrs...).DebugContext(ctx, "fleet node rejected command as BUSY",
			"sends", sends, "final_ack_code", ack.GetCode().String(), "error", err)
	}
	return ack, err
}

// acquireLeavingBudget stops waiting for a permit once a send could no longer
// keep minRemaining of the deadline.
func acquireLeavingBudget(ctx context.Context, minRemaining time.Duration, acquire func(context.Context) (func(), error)) (func(), error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return acquire(ctx)
	}
	acquireCtx, cancel := context.WithDeadline(ctx, deadline.Add(-minRemaining))
	defer cancel()
	return acquire(acquireCtx)
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
