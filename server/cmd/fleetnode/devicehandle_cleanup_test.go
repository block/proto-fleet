package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type cleanupTestDriver struct {
	sdk.Driver
	created         atomic.Int32
	closed          atomic.Int32
	rejectCreation  atomic.Bool
	canceledCloses  atomic.Int32
	cancelOperation context.CancelFunc
}

func (d *cleanupTestDriver) NewDevice(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	d.created.Add(1)
	if d.rejectCreation.Load() {
		return sdk.NewDeviceResult{}, fmt.Errorf("create test device: %w", grpcstatus.Error(codes.Unavailable, "identity endpoint unavailable before registration"))
	}
	return sdk.NewDeviceResult{Device: &cleanupTestDevice{id: id, owner: d}}, nil
}

type cleanupTestDevice struct {
	sdk.Device
	id    string
	owner *cleanupTestDriver
}

func (d *cleanupTestDevice) ID() string { return d.id }
func (d *cleanupTestDevice) Status(context.Context) (sdk.DeviceMetrics, error) {
	if d.owner.cancelOperation != nil {
		d.owner.cancelOperation()
	}
	return sdk.DeviceMetrics{DeviceID: d.id, Timestamp: time.Now(), Health: sdk.HealthHealthyActive}, nil
}
func (d *cleanupTestDevice) Reboot(context.Context) error {
	if d.owner.cancelOperation != nil {
		d.owner.cancelOperation()
	}
	return nil
}
func (d *cleanupTestDevice) Close(ctx context.Context) error {
	if ctx.Err() != nil {
		d.owner.canceledCloses.Add(1)
	}
	d.owner.closed.Add(1)
	return nil
}

func newCleanupTestPool(t *testing.T, capacity int) *deviceHandlePool {
	t.Helper()
	pool := newDeviceHandlePool(t.Context(), capacity)
	pool.closeTimeout = 20 * time.Millisecond
	pool.retryInterval = 5 * time.Millisecond
	pool.maxRetryInterval = 10 * time.Millisecond
	t.Cleanup(pool.shutdown)
	return pool
}

func runCleanupTestOperation(t *testing.T, ctx context.Context, operation string, run *RunCmd, fetcher *pluginTelemetryFetcher) pb.AckCode {
	t.Helper()
	if operation == "command" {
		ack := &captureAcker{}
		run.handleMinerCommand(ctx, nil, ack, "cleanup-command", withTarget(&pb.MinerCommand{
			Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}},
		}), discardLogger(t))
		return ack.only(t).GetCode()
	}
	result, err := fetcher.Fetch(ctx, handleTestTelemetryRequest())
	if err != nil {
		var commandErr *commandError
		require.ErrorAs(t, err, &commandErr)
		return commandErr.code
	}
	requirePhysicalTelemetryIdentity(t, result)
	return pb.AckCode_ACK_CODE_OK
}

func newCleanupTestOperations(t *testing.T, impl sdk.Driver, pool *deviceHandlePool, interceptor grpc.UnaryServerInterceptor) (*RunCmd, *pluginTelemetryFetcher) {
	t.Helper()
	driver := newHandleTestGRPCDriver(t, impl, grpc.UnaryInterceptor(interceptor))
	run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}, deviceHandles: pool}
	fetcher := newHandleTestTelemetryFetcher(t, driver)
	fetcher.deviceHandles = pool
	return run, fetcher
}

func TestSuccessfulOperationRetriesCloseBeforeRegistryRemoval(t *testing.T) {
	for _, operation := range []string{"command", "telemetry"} {
		for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded} {
			t.Run(operation+"/"+code.String(), func(t *testing.T) {
				impl := &cleanupTestDriver{}
				var closeAttempts atomic.Int32
				interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
					if strings.HasSuffix(info.FullMethod, "/CloseDevice") && closeAttempts.Add(1) <= 2 {
						// The SDK registry still owns the successfully created device:
						// unlike a lost response, this failure occurs before removal.
						return nil, grpcstatus.Error(code, "close transport failed before handler")
					}
					return handler(ctx, req)
				}
				pool := newCleanupTestPool(t, 1)
				run, fetcher := newCleanupTestOperations(t, impl, pool, interceptor)
				require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), operation, run, fetcher))
				require.Eventually(t, func() bool { return impl.closed.Load() == 1 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond,
					"a failed Close must retain ownership and retry the original registration")
				require.EqualValues(t, 1, impl.created.Load(), "cleanup must never recreate or replay the operation")
				require.EqualValues(t, 3, closeAttempts.Load())
			})
		}
	}
}

func TestFailedCloseBoundsRegistrationsAcrossCommandsAndTelemetry(t *testing.T) {
	impl := &cleanupTestDriver{}
	pool := newCleanupTestPool(t, 2)
	var recovered atomic.Bool
	var closeAttempts atomic.Int32
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasSuffix(info.FullMethod, "/CloseDevice") {
			closeAttempts.Add(1)
			if !recovered.Load() {
				return nil, grpcstatus.Error(codes.Unavailable, "plugin close transport unavailable")
			}
		}
		return handler(ctx, req)
	}
	run, fetcher := newCleanupTestOperations(t, impl, pool, interceptor)
	operationCtx, cancelOperation := context.WithCancel(t.Context())
	defer cancelOperation()
	for _, operation := range []string{"command", "telemetry"} {
		require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, operationCtx, operation, run, fetcher))
	}
	require.Eventually(t, func() bool { return closeAttempts.Load() >= 4 }, time.Second, 5*time.Millisecond)
	// The originating control session can end while the plugin keeps running.
	// Its retained registrations must still count against a later session.
	cancelOperation()
	for range 3 {
		for _, operation := range []string{"command", "telemetry"} {
			require.Equal(t, pb.AckCode_ACK_CODE_BUSY, runCleanupTestOperation(t, t.Context(), operation, run, fetcher),
				"unacknowledged registrations must retain shared admission capacity")
		}
	}
	require.EqualValues(t, 2, impl.created.Load(), "BUSY must precede the SDK NewDevice RPC")
	require.Zero(t, impl.closed.Load(), "failed transport must not be mistaken for registry removal")
	recovered.Store(true)
	require.Eventually(t, func() bool { return impl.closed.Load() == 2 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
	for _, operation := range []string{"command", "telemetry"} {
		require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), operation, run, fetcher))
	}
	require.Eventually(t, func() bool { return impl.closed.Load() == 4 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
}

func TestLostCloseResponseReleasesCapacityAfterNotFound(t *testing.T) {
	for _, operation := range []string{"command", "telemetry"} {
		t.Run(operation, func(t *testing.T) {
			impl := &cleanupTestDriver{}
			pool := newCleanupTestPool(t, 1)
			var closeAttempts atomic.Int32
			interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				response, err := handler(ctx, req)
				if strings.HasSuffix(info.FullMethod, "/CloseDevice") && closeAttempts.Add(1) == 1 {
					// This time removal completed; retry must accept the real SDK's
					// NotFound without invoking the underlying device.Close twice.
					return nil, grpcstatus.Error(codes.Unavailable, "lost Close response after registry removal")
				}
				return response, err
			}
			run, fetcher := newCleanupTestOperations(t, impl, pool, interceptor)
			require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), operation, run, fetcher))
			require.Eventually(t, func() bool { return closeAttempts.Load() == 2 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
			require.EqualValues(t, 1, impl.closed.Load())
			require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), operation, run, fetcher))
			require.Eventually(t, func() bool { return impl.closed.Load() == 2 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
		})
	}
}

type contextIgnoringCleanup struct {
	calls   atomic.Int32
	started chan context.Context
	release chan struct{}
}

func (c *contextIgnoringCleanup) Close(ctx context.Context) error {
	c.calls.Add(1)
	c.started <- ctx
	<-c.release
	return nil
}

func TestContextIgnoringCloseRetainsBoundedWorkerAndAdmission(t *testing.T) {
	pool := newCleanupTestPool(t, 1)
	lease, err := pool.acquire()
	require.NoError(t, err)
	closer := &contextIgnoringCleanup{started: make(chan context.Context, 1), release: make(chan struct{})}
	defer close(closer.release)
	lease.close(closer)
	var closeCtx context.Context
	select {
	case closeCtx = <-closer.started:
	case <-time.After(time.Second):
		t.Fatal("close worker did not start")
	}
	select {
	case <-closeCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("close attempt did not receive a deadline")
	}
	_, err = pool.acquire()
	require.Error(t, err, "context-ignoring Close must retain its admission slot")
	require.Never(t, func() bool { return closer.calls.Load() > 1 || len(pool.slots) != 1 }, 80*time.Millisecond, 5*time.Millisecond,
		"a timed-out attempt that ignores context must not spawn more workers or free admission")
	pool.shutdown()
	require.Error(t, closeCtx.Err())
	require.EqualValues(t, 1, closer.calls.Load())
}

func TestAbsentFailedRegistrationsDoNotExhaustCleanupCapacity(t *testing.T) {
	impl := &cleanupTestDriver{}
	impl.rejectCreation.Store(true)
	pool := newCleanupTestPool(t, 1)
	interceptor := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, req)
	}
	run, fetcher := newCleanupTestOperations(t, impl, pool, interceptor)
	// More failures than capacity, through both callers. The real SDK returns
	// NotFound for cleanup because NewDevice failed before registry insertion.
	for _, operation := range []string{"command", "telemetry"} {
		code := runCleanupTestOperation(t, t.Context(), operation, run, fetcher)
		require.NotEqual(t, pb.AckCode_ACK_CODE_OK, code)
		require.NotEqual(t, pb.AckCode_ACK_CODE_BUSY, code, "ordinary failed creation must release its reservation after cleanup grace")
		require.Empty(t, pool.slots)
	}
	require.EqualValues(t, 2, impl.created.Load())
	impl.rejectCreation.Store(false)
	require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), "command", run, fetcher))
	require.Eventually(t, func() bool { return impl.closed.Load() == 1 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
	require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), "telemetry", run, fetcher))
	require.Eventually(t, func() bool { return impl.closed.Load() == 2 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
}

func TestCallerCancellationDoesNotCancelOwnedCleanup(t *testing.T) {
	for _, operation := range []string{"command", "telemetry"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			impl := &cleanupTestDriver{cancelOperation: cancel}
			pool := newCleanupTestPool(t, 1)
			interceptor := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				return handler(ctx, req)
			}
			run, fetcher := newCleanupTestOperations(t, impl, pool, interceptor)
			_ = runCleanupTestOperation(t, ctx, operation, run, fetcher)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Eventually(t, func() bool { return impl.closed.Load() == 1 && len(pool.slots) == 0 }, time.Second, 5*time.Millisecond)
			require.Zero(t, impl.canceledCloses.Load(), "cleanup must use the plugin lifecycle, not the canceled operation")
		})
	}
}
