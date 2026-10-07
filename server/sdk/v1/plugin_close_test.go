package sdk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
)

type observedCloseWaitContext struct {
	context.Context //nolint:containedctx // Test decorator observes a waiter joining the shared close attempt.
	waiting         chan struct{}
	once            sync.Once
}

func (c *observedCloseWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestDriverGRPCServerCloseRetainsContextIgnoringBackend(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	device := fakeDevice{closeFunc: func(context.Context) error {
		calls.Add(1)
		close(started)
		<-release
		return nil
	}}
	server := &DriverGRPCServer{devices: map[string]Device{"device-123": device}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() {
		_, err := server.CloseDevice(ctx, &pb.DeviceRef{DeviceId: "device-123"})
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backend Close did not start")
	}
	cancel()

	for range 3 {
		retryCtx, retryCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		_, err := server.CloseDevice(retryCtx, &pb.DeviceRef{DeviceId: "device-123"})
		retryCancel()
		require.Equal(t, codes.DeadlineExceeded, status.Code(err), "a pending backend Close must not certify absence")
	}
	assert.EqualValues(t, 1, calls.Load(), "retries must not overlap backend cleanup")
	_, err := server.DeviceStatus(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.Equal(t, codes.NotFound, status.Code(err), "closing handles must reject ordinary device operations")
	select {
	case <-first:
		t.Fatal("first Close returned before its backend finished")
	default:
	}
}

func TestDriverGRPCServerConcurrentCloseSharesCompletion(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	device := fakeDevice{closeFunc: func(context.Context) error {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	}}
	server := &DriverGRPCServer{devices: map[string]Device{"device-123": device}}
	first, retry := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backend Close did not start")
	}
	waitCtx := &observedCloseWaitContext{Context: t.Context(), waiting: make(chan struct{})}
	go func() {
		_, err := server.CloseDevice(waitCtx, &pb.DeviceRef{DeviceId: "device-123"})
		retry <- err
	}()
	select {
	case <-waitCtx.waiting:
		// Done is read only after the retry joins the in-progress attempt.
	case err := <-retry:
		close(release)
		t.Fatalf("retry returned before cleanup completed: %v", err)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("retry did not join the in-progress close")
	}
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, <-retry)
	assert.EqualValues(t, 1, calls.Load())
	_, err := server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.Equal(t, codes.NotFound, status.Code(err), "absence is safe only after backend cleanup completes")
	server.mu.RLock()
	defer server.mu.RUnlock()
	assert.Empty(t, server.devices)
	assert.Empty(t, server.closingDevices)
}

func TestDriverGRPCServerFailedCloseRemainsRetryable(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "backend failure", err: errors.New("cleanup failed"), code: codes.Unknown},
		{name: "backend not found", err: NewErrorDeviceNotFound("device-123"), code: codes.Unavailable},
	} {
		t.Run(failure.name, func(t *testing.T) {
			var calls atomic.Int32
			device := fakeDevice{closeFunc: func(context.Context) error {
				if calls.Add(1) == 1 {
					return failure.err
				}
				return nil
			}}
			server := &DriverGRPCServer{devices: map[string]Device{"device-123": device}}
			_, err := server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
			require.Equal(t, failure.code, status.Code(err))
			_, err = server.DeviceStatus(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
			require.Equal(t, codes.NotFound, status.Code(err))
			_, err = server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
			require.NoError(t, err)
			assert.EqualValues(t, 2, calls.Load())
			_, err = server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
			require.Equal(t, codes.NotFound, status.Code(err))
		})
	}
}

func TestDriverGRPCServerRejectedCreationRetainsBlockedCleanup(t *testing.T) {
	for _, reason := range []string{"canceled", "mismatched ID"} {
		t.Run(reason, func(t *testing.T) {
			var calls atomic.Int32
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			server := &DriverGRPCServer{
				devices: make(map[string]Device),
				Impl: fakeDriver{newDeviceFunc: func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
					return NewDeviceResult{Device: fakeDevice{closeFunc: func(context.Context) error {
						calls.Add(1)
						close(started)
						<-release
						return nil
					}}}, nil
				}},
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			deviceID := "device-123"
			if reason == "canceled" {
				cancel()
			} else {
				deviceID = "mismatched-request"
			}
			created := make(chan error, 1)
			go func() {
				_, err := server.NewDevice(ctx, &pb.NewDeviceRequest{DeviceId: deviceID, Info: &pb.DeviceInfo{}, Secret: &pb.SecretBundle{}})
				created <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("rejected creation cleanup did not start")
			}
			_, err := server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: deviceID})
			require.Equal(t, codes.Unavailable, status.Code(err), "rejected creation must remain owned while backend cleanup is running")
			assert.EqualValues(t, 1, calls.Load())
			_, err = server.DeviceStatus(t.Context(), &pb.DeviceRef{DeviceId: deviceID})
			require.Equal(t, codes.NotFound, status.Code(err), "rejected creation must never become operational")
			select {
			case <-created:
				t.Fatal("creation returned before its backend cleanup finished")
			default:
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case err := <-created:
				if reason == "canceled" {
					require.Equal(t, codes.Canceled, status.Code(err))
				} else {
					require.ErrorContains(t, err, "device ID mismatch")
				}
			case <-time.After(time.Second):
				t.Fatal("creation did not return after cleanup")
			}
			_, err = server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: deviceID})
			require.Equal(t, codes.NotFound, status.Code(err), "completed rejected cleanup must release its reservation")
		})
	}
}

func TestDriverGRPCServerNewDeviceRejectsClosingIDUntilCompletion(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var oldClosed, newClosed, created atomic.Int32
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": fakeDevice{closeFunc: func(context.Context) error {
			oldClosed.Add(1)
			close(started)
			<-release
			return nil
		}}},
		Impl: fakeDriver{newDeviceFunc: func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
			created.Add(1)
			return NewDeviceResult{Device: fakeDevice{closeFunc: func(context.Context) error { newClosed.Add(1); return nil }}}, nil
		}},
	}
	closed := make(chan error, 1)
	go func() {
		_, err := server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
		closed <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backend Close did not start")
	}
	req := &pb.NewDeviceRequest{DeviceId: "device-123", Info: &pb.DeviceInfo{}, Secret: &pb.SecretBundle{}}
	_, err := server.NewDevice(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.Zero(t, created.Load(), "must reject before constructing a replacement")
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-closed)
	_, err = server.NewDevice(t.Context(), req)
	require.NoError(t, err, "completed cleanup must permit a fresh registration")
	_, err = server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, oldClosed.Load())
	assert.EqualValues(t, 1, newClosed.Load())
}

func TestDriverGRPCServerCreationReservationSerializesReplacementAndClose(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var oldClosed, newClosed, creations atomic.Int32
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": fakeDevice{closeFunc: func(context.Context) error { oldClosed.Add(1); return nil }}},
		Impl: fakeDriver{newDeviceFunc: func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
			creations.Add(1)
			close(started)
			<-release
			return NewDeviceResult{Device: fakeDevice{closeFunc: func(context.Context) error { newClosed.Add(1); return nil }}}, nil
		}},
	}
	req := &pb.NewDeviceRequest{DeviceId: "device-123", Info: &pb.DeviceInfo{}, Secret: &pb.SecretBundle{}}
	created := make(chan error, 1)
	go func() {
		_, err := server.NewDevice(t.Context(), req)
		created <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("driver creation did not start")
	}
	_, err := server.NewDevice(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err), "concurrent construction must not bypass close ownership")
	_, err = server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.Equal(t, codes.Unavailable, status.Code(err), "Close must not race an in-progress replacement")
	assert.Zero(t, oldClosed.Load())
	assert.Zero(t, newClosed.Load())
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-created, "preserve existing active-replacement semantics")
	_, err = server.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, creations.Load())
	assert.Zero(t, oldClosed.Load(), "replacement behavior is unchanged")
	assert.EqualValues(t, 1, newClosed.Load(), "Close must target the completed replacement")
}
