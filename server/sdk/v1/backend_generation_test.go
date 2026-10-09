package sdk

import (
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestScopedBackendGenerationRejectsReplacementOnActualGRPCRegistry(t *testing.T) {
	var writes atomic.Int32
	server := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
		return NewDeviceResult{Device: fakeDevice{startMiningFunc: func(context.Context) error { writes.Add(1); return nil }}}, nil
	})
	listener := bufconn.Listen(1 << 20)
	wire := grpc.NewServer()
	pb.RegisterDriverServer(wire, server)
	go func() { _ = wire.Serve(listener) }()
	t.Cleanup(wire.Stop)
	conn, err := grpc.NewClient("passthrough:///sdk-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := &DriverGRPCClient{client: pb.NewDriverClient(conn)}
	old, err := client.GetOrCreateDevice(t.Context(), "device-123", DeviceInfo{}, SecretBundle{}, "old-binding")
	require.NoError(t, err)
	current, err := client.GetOrCreateDevice(t.Context(), "device-123", DeviceInfo{}, SecretBundle{}, "new-binding")
	require.NoError(t, err)
	require.True(t, IsBackendGenerationRejected(old.Device.StartMining(t.Context())))
	require.True(t, IsBackendGenerationRejected(old.Device.Unpair(t.Context())), "a delayed retired worker must not unpair a replacement")
	require.True(t, IsBackendGenerationRejected(old.Device.Close(t.Context())), "stale cleanup must not close the replacement")
	require.True(t, IsBackendGenerationRejected(client.CloseDevice(t.Context(), "device-123")), "ID-only cleanup cannot select a scoped replacement")
	require.Zero(t, writes.Load())
	require.NoError(t, current.Device.StartMining(t.Context()))
	require.EqualValues(t, 1, writes.Load())
	// A delayed old resolver can register after current. The current facade must
	// reject rather than select that old binding even though its ID is unchanged.
	delayed, err := client.GetOrCreateDevice(t.Context(), "device-123", DeviceInfo{}, SecretBundle{}, "old-binding")
	require.NoError(t, err)
	require.True(t, IsBackendGenerationRejected(current.Device.StartMining(t.Context())))
	require.True(t, IsBackendGenerationRejected(old.Device.StartMining(t.Context())), "even identical configuration gets a fresh backend generation after replacement")
	require.NoError(t, delayed.Device.StartMining(t.Context()))
	require.EqualValues(t, 2, writes.Load())
}

func TestBackendGenerationAdmissionMarkerIsSpecific(t *testing.T) {
	require.True(t, IsBackendGenerationRejected(backendGenerationRejected()))
	require.False(t, IsBackendGenerationRejected(status.Error(codes.FailedPrecondition, "driver failed after I/O")))
	require.False(t, IsBackendGenerationRejected(fmt.Errorf("transport failure")))
}

type namedBackend struct {
	fakeDevice
	id string
}

func (d namedBackend) ID() string { return d.id }

func TestScopedCreationDoesNotBlockCollidingIDsAndCanceledWaiters(t *testing.T) {
	oldStripe := func(id string) uint64 { hash := fnv.New64a(); _, _ = hash.Write([]byte(id)); return hash.Sum64() % 64 }
	other := ""
	for i := 0; other == ""; i++ {
		candidate := fmt.Sprintf("miner-%d", i)
		if oldStripe(candidate) == oldStripe("device-123") {
			other = candidate
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	server := scopedServer(func(_ context.Context, id string, _ DeviceInfo, _ SecretBundle) (NewDeviceResult, error) {
		if id == "device-123" {
			close(started)
			<-release
		}
		return NewDeviceResult{Device: namedBackend{id: id}}, nil
	})
	done := make(chan error, 1)
	go func() { _, err := server.NewDevice(t.Context(), scopedRequest()); done <- err }()
	<-started
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	otherReq := scopedRequest()
	otherReq.DeviceId = other
	deadline, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := server.NewDevice(deadline, otherReq)
	require.NoError(t, err, "an old-stripe collision must not block an unrelated backend")
	waiting, stopWaiting := context.WithCancel(t.Context())
	waitingDone := make(chan error, 1)
	waitCtx := &observedCloseWaitContext{Context: waiting, waiting: make(chan struct{})}
	go func() { _, err := server.NewDevice(waitCtx, scopedRequest()); waitingDone <- err }()
	<-waitCtx.waiting
	stopWaiting()
	select {
	case err := <-waitingDone:
		require.Equal(t, codes.Canceled, status.Code(err))
	case <-time.After(time.Second):
		t.Fatal("canceled waiter remained blocked behind device creation")
	}
	server.reuseLocks.mu.Lock()
	require.Len(t, server.reuseLocks.entries, 1, "completed and canceled IDs must release idle lock entries")
	server.reuseLocks.mu.Unlock()
	close(release)
	released = true
	require.NoError(t, <-done)
	server.reuseLocks.mu.Lock()
	require.Empty(t, server.reuseLocks.entries)
	server.reuseLocks.mu.Unlock()
}

func TestScopedGenerationCheckedByEveryUnaryDeviceEndpoint(t *testing.T) {
	server := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
		return NewDeviceResult{Device: fakeDevice{}}, nil
	})
	old, err := server.NewDevice(t.Context(), scopedRequest())
	require.NoError(t, err)
	replacement := scopedRequest()
	replacement.ReuseScopeKey = "replacement"
	_, err = server.NewDevice(t.Context(), replacement)
	require.NoError(t, err)
	ref := &pb.DeviceRef{DeviceId: old.DeviceId, BackendGeneration: old.BackendGeneration}
	desired := true
	cases := map[string]func() error{
		"DescribeDevice": func() error {
			_, e := server.DescribeDevice(t.Context(), &pb.DescribeDeviceRequest{DeviceId: ref.DeviceId, BackendGeneration: ref.BackendGeneration})
			return e
		},
		"DeviceStatus": func() error { _, e := server.DeviceStatus(t.Context(), ref); return e },
		"GetErrors":    func() error { _, e := server.GetErrors(t.Context(), ref); return e },
		"CloseDevice":  func() error { _, e := server.CloseDevice(t.Context(), ref); return e },
		"StartMining":  func() error { _, e := server.StartMining(t.Context(), ref); return e },
		"StopMining":   func() error { _, e := server.StopMining(t.Context(), ref); return e },
		"BlinkLED":     func() error { _, e := server.BlinkLED(t.Context(), ref); return e },
		"Reboot":       func() error { _, e := server.Reboot(t.Context(), ref); return e },
		"Unpair":       func() error { _, e := server.Unpair(t.Context(), ref); return e },
		"SetCoolingMode": func() error {
			_, e := server.SetCoolingMode(t.Context(), &pb.SetCoolingModeRequest{Ref: ref})
			return e
		},
		"GetCoolingMode": func() error { _, e := server.GetCoolingMode(t.Context(), ref); return e },
		"SetPowerTarget": func() error {
			_, e := server.SetPowerTarget(t.Context(), &pb.SetPowerTargetRequest{Ref: ref})
			return e
		},
		"UpdateMiningPools": func() error {
			_, e := server.UpdateMiningPools(t.Context(), &pb.UpdateMiningPoolsRequest{Ref: ref})
			return e
		},
		"GetMiningPools": func() error {
			_, e := server.GetMiningPools(t.Context(), &pb.GetMiningPoolsRequest{Ref: ref})
			return e
		},
		"GetStoredMiningPools": func() error {
			_, e := server.GetStoredMiningPools(t.Context(), &pb.GetStoredMiningPoolsRequest{Ref: ref})
			return e
		},
		"DownloadLogs": func() error { _, e := server.DownloadLogs(t.Context(), &pb.DownloadLogsRequest{Ref: ref}); return e },
		"UpdateFirmware": func() error {
			_, e := server.UpdateFirmware(t.Context(), &pb.UpdateFirmwareRequest{Ref: ref})
			return e
		},
		"GetFirmwareUpdateStatus": func() error { _, e := server.GetFirmwareUpdateStatus(t.Context(), ref); return e },
		"UpdateMinerPassword": func() error {
			_, e := server.UpdateMinerPassword(t.Context(), &pb.UpdateMinerPasswordRequest{Ref: ref})
			return e
		},
		"Curtail":   func() error { _, e := server.Curtail(t.Context(), &pb.CurtailRequest{Ref: ref}); return e },
		"Uncurtail": func() error { _, e := server.Uncurtail(t.Context(), &pb.UncurtailRequest{Ref: ref}); return e },
		"ApplyCurtailmentConfig": func() error {
			_, e := server.ApplyCurtailmentConfig(t.Context(), &pb.ApplyCurtailmentConfigRequest{Ref: ref, Config: &pb.CurtailmentConfig{}})
			return e
		},
		"GetTimeSeriesData": func() error {
			_, e := server.GetTimeSeriesData(t.Context(), &pb.GetTimeSeriesDataRequest{Ref: ref})
			return e
		},
		"GetDeviceWebViewURL": func() error {
			_, e := server.GetDeviceWebViewURL(t.Context(), &pb.GetDeviceWebViewURLRequest{Ref: ref})
			return e
		},
		"SetContainerControl": func() error {
			_, e := server.SetContainerControl(t.Context(), &pb.SetContainerControlRequest{Ref: ref, Name: "coolant_pump", Enabled: &desired})
			return e
		},
		"SetTankMining": func() error {
			_, e := server.SetTankMining(t.Context(), &pb.SetTankMiningRequest{Ref: ref, Positions: []int32{1}, Mining: &desired})
			return e
		},
		"BatchStatus": func() error {
			_, e := server.BatchStatus(t.Context(), &pb.BatchStatusRequest{Refs: []*pb.DeviceRef{ref}})
			return e
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			require.True(t, IsBackendGenerationRejected(call()), "must reject before capability checks or backend I/O")
		})
	}
}

func TestClosedScopedBackendRejectsBeforeDispatch(t *testing.T) {
	server := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
		return NewDeviceResult{Device: fakeDevice{}}, nil
	})
	response, err := server.NewDevice(t.Context(), scopedRequest())
	require.NoError(t, err)
	ref := &pb.DeviceRef{DeviceId: response.DeviceId, BackendGeneration: response.BackendGeneration}
	_, err = server.CloseDevice(t.Context(), ref)
	require.NoError(t, err)
	_, err = server.StartMining(t.Context(), ref)
	require.True(t, IsBackendGenerationRejected(err))
	_, err = server.CloseDevice(t.Context(), ref)
	require.True(t, IsBackendGenerationRejected(err))
	_, err = server.StartMining(t.Context(), &pb.DeviceRef{DeviceId: response.DeviceId})
	require.Equal(t, codes.NotFound, status.Code(err))
}
