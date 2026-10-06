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
	// A canceled or timed-out NewDevice may still register shortly after its
	// caller stops waiting, so NotFound proves absence only after this grace.
	creationGrace         time.Duration
	creationRetryInterval time.Duration
}

func newDeviceHandlePool(parent context.Context, capacity int) *deviceHandlePool {
	ctx, cancel := context.WithCancel(parent)
	return &deviceHandlePool{
		ctx: ctx, cancel: cancel, slots: make(chan struct{}, capacity),
		closeTimeout: 5 * time.Second, retryInterval: 100 * time.Millisecond, maxRetryInterval: 5 * time.Second,
		creationGrace: time.Second, creationRetryInterval: 10 * time.Millisecond,
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

// getDeviceHandlePool returns the node's only handle pool. Plugin bootstrap
// gives this pool to telemetry and shuts it down with the plugin processes.
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
	l.cleanupOnce.Do(func() { go l.retainUntilRemoved(device.Close, closedOrAbsent, l.pool.retryInterval) })
	return l.firstAttemptDone
}

// NewDevice completed successfully before this worker was started, so
// NotFound confirms this registration no longer exists in the plugin.
func closedOrAbsent(err error) bool {
	return err == nil || grpcstatus.Code(err) == codes.NotFound
}

// releaseAfterFailedCreation frees the reservation unless the failed NewDevice
// may have left handleID registered. A cleanup worker then keeps it until the
// plugin confirms the handle is gone, so the caller can return without waiting.
func (l *deviceHandleLease) releaseAfterFailedCreation(ctx context.Context, driver sdk.Driver, handleID string, err error) {
	cleaner, ok := driver.(deviceHandleCleaner)
	mayStillRegister := creationMayStillRegister(ctx, err)
	// The SDK registers a handle only while its request context is live. After a
	// live-context Unavailable, registration either completed with a lost
	// response or never happened, so the first NotFound confirms absence.
	if !ok || (!mayStillRegister && grpcstatus.Code(err) != codes.Unavailable) {
		l.release()
		return
	}
	graceEnds := time.Now().Add(l.pool.creationGrace)
	removed := func(closeErr error) bool {
		if closeErr == nil {
			return true
		}
		return grpcstatus.Code(closeErr) == codes.NotFound && (!mayStillRegister || !time.Now().Before(graceEnds))
	}
	closeDevice := func(closeCtx context.Context) error { return cleaner.CloseDevice(closeCtx, handleID) }
	l.cleanupOnce.Do(func() { go l.retainUntilRemoved(closeDevice, removed, l.pool.creationRetryInterval) })
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

// retainUntilRemoved keeps the lease's capacity until removed accepts a close
// result or the plugin shuts down, retrying with capped exponential backoff.
func (l *deviceHandleLease) retainUntilRemoved(attempt func(context.Context) error, removed func(error) bool, delay time.Duration) {
	defer l.release()
	firstAttempt := true
	defer func() {
		if firstAttempt {
			close(l.firstAttemptDone)
		}
	}()
	warned := false
	for l.pool.ctx.Err() == nil {
		closeCtx, cancel := context.WithTimeout(l.pool.ctx, l.pool.closeTimeout)
		// Call directly in this one worker. A context-ignoring implementation
		// keeps its lease; timing out must not spawn another Close for it.
		err := attempt(closeCtx)
		cancel()
		if firstAttempt {
			firstAttempt = false
			close(l.firstAttemptDone)
		}
		if removed(err) {
			return
		}
		if !warned && grpcstatus.Code(err) != codes.NotFound && l.pool.ctx.Err() == nil {
			warned = true
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

// creationMayStillRegister reports whether a failed NewDevice may still be
// running in the plugin and register its handle after the caller gave up.
func creationMayStillRegister(ctx context.Context, err error) bool {
	code := grpcstatus.Code(err)
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		code == codes.Canceled || code == codes.DeadlineExceeded
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
