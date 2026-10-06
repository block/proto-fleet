package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Reserve capacity for active operations and confirmed registrations awaiting
// cleanup. A plugin outage must apply backpressure before creating more handles.
const maxOwnedDeviceHandles = 2 * commandPoolSize

type deviceHandlePool struct {
	ctx              context.Context //nolint:containedctx // Owns cleanup workers for the plugin-process lifetime, across control sessions.
	cancel           context.CancelFunc
	slots            chan struct{}
	closeTimeout     time.Duration
	retryInterval    time.Duration
	maxRetryInterval time.Duration
}

func newDeviceHandlePool(parent context.Context, capacity int) *deviceHandlePool {
	ctx, cancel := context.WithCancel(parent)
	return &deviceHandlePool{
		ctx: ctx, cancel: cancel, slots: make(chan struct{}, capacity),
		closeTimeout: 5 * time.Second, retryInterval: 100 * time.Millisecond, maxRetryInterval: 5 * time.Second,
	}
}

func (p *deviceHandlePool) shutdown() { p.cancel() }

func (p *deviceHandlePool) acquire() (*deviceHandleLease, error) {
	if p.ctx.Err() != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BUSY, "plugin device cleanup is shutting down")
	}
	select {
	case p.slots <- struct{}{}:
		return &deviceHandleLease{pool: p, firstAttemptDone: make(chan struct{})}, nil
	default:
		return nil, cmdErr(pb.AckCode_ACK_CODE_BUSY, "plugin device handle capacity exhausted; waiting for cleanup")
	}
}

func (r *RunCmd) getDeviceHandlePool() *deviceHandlePool {
	r.deviceHandlesOnce.Do(func() {
		if r.deviceHandles == nil {
			r.deviceHandles = newDeviceHandlePool(context.Background(), maxOwnedDeviceHandles)
		}
	})
	return r.deviceHandles
}

type deviceHandleCloser interface {
	Close(ctx context.Context) error
}

type deviceHandleLease struct {
	pool             *deviceHandlePool
	cleanupOnce      sync.Once
	releaseOnce      sync.Once
	firstAttemptDone chan struct{}
}

func (l *deviceHandleLease) release() {
	l.releaseOnce.Do(func() { <-l.pool.slots })
}

func (l *deviceHandleLease) close(device deviceHandleCloser) <-chan struct{} {
	l.cleanupOnce.Do(func() { go l.cleanup(device) })
	return l.firstAttemptDone
}

// Commands already acknowledge their result before deferred cleanup. Preserve
// prompt normal cleanup, but leave failed or stuck closes owned by the worker.
func (l *deviceHandleLease) closeAndWait(device deviceHandleCloser) {
	done := l.close(device)
	timer := time.NewTimer(l.pool.closeTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-l.pool.ctx.Done():
	}
}

func (l *deviceHandleLease) cleanup(device deviceHandleCloser) {
	defer l.release()
	firstAttempt := true
	defer func() {
		if firstAttempt {
			close(l.firstAttemptDone)
		}
	}()
	delay := l.pool.retryInterval
	for l.pool.ctx.Err() == nil {
		closeCtx, cancel := context.WithTimeout(l.pool.ctx, l.pool.closeTimeout)
		// Call directly in this one worker. A context-ignoring implementation
		// keeps its lease; timing out must not spawn another Close for it.
		err := device.Close(closeCtx)
		cancel()
		initialAttempt := firstAttempt
		if firstAttempt {
			firstAttempt = false
			close(l.firstAttemptDone)
		}
		// NewDevice completed successfully before this worker was started, so
		// NotFound confirms this registration no longer exists in the plugin.
		if err == nil || grpcstatus.Code(err) == codes.NotFound {
			return
		}
		if initialAttempt && l.pool.ctx.Err() == nil {
			slog.Warn("plugin device close failed; retaining handle for retry", "err", err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-l.pool.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, l.pool.maxRetryInterval)
	}
}

type deviceHandleCleaner interface {
	CloseDevice(ctx context.Context, deviceID string) error
}

func cleanupUncertainDeviceCreation(ctx context.Context, driver sdk.Driver, handleID string, err error) {
	code := grpcstatus.Code(err)
	uncertain := ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		code == codes.Canceled || code == codes.DeadlineExceeded || code == codes.Unavailable
	if cleaner, ok := driver.(deviceHandleCleaner); ok && uncertain {
		// The registration may exist even though its response was canceled or lost.
		closeUncertainDevice(ctx, cleaner, handleID)
	}
}

func closeUncertainDevice(ctx context.Context, cleaner deviceHandleCleaner, deviceID string) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	for {
		if cleaner.CloseDevice(closeCtx, deviceID) == nil {
			return
		}
		// Keep retrying even after NotFound: NewDevice may still be registering.
		select {
		case <-closeCtx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}
