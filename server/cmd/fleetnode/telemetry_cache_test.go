package main

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestTelemetryReusesDeviceAcrossPolls(t *testing.T) {
	impl := &cleanupTestDriver{}
	pool := newCleanupTestPool(t, 4)
	fetcher := newHandleTestTelemetryFetcher(t, newHandleTestGRPCDriver(t, impl), pool)
	for range 2 {
		result, err := fetcher.Fetch(t.Context(), handleTestTelemetryRequest())
		require.NoError(t, err)
		requirePhysicalTelemetryIdentity(t, result)
	}
	require.EqualValues(t, 1, impl.created.Load(), "polls must reuse the authenticated SDK device")
	require.Zero(t, impl.closed.Load(), "a successful poll must keep the device alive")
	fetcher.clearCachedDevices()
	require.Eventually(t, func() bool { return impl.closed.Load() == 1 }, time.Second, time.Millisecond)
}

type telemetryTestSecrets struct{ nodeSecretProvider }

func (telemetryTestSecrets) SecretBundle(target *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error) {
	return sdk.SecretBundle{Kind: sdk.UsernamePassword{Username: "user", Password: string(target.GetCredentialPassword())}}, nil
}

func TestTelemetryReplacesChangedDevice(t *testing.T) {
	for name, change := range map[string]func(*telemetrypb.FleetNodeTelemetryRequest){
		"address":     func(r *telemetrypb.FleetNodeTelemetryRequest) { r.IpAddress = "10.0.0.6" },
		"credentials": func(r *telemetrypb.FleetNodeTelemetryRequest) { r.CredentialPassword = []byte("changed") },
		"identity":    func(r *telemetrypb.FleetNodeTelemetryRequest) { r.MacAddress = "00:11:22:33:44:55" },
	} {
		t.Run(name, func(t *testing.T) {
			impl := &cleanupTestDriver{}
			fetcher := newHandleTestTelemetryFetcher(t, newHandleTestGRPCDriver(t, impl), newCleanupTestPool(t, 4))
			fetcher.minerSecrets = telemetryTestSecrets{}
			req := handleTestTelemetryRequest()
			_, err := fetcher.Fetch(t.Context(), req)
			require.NoError(t, err)
			replacement := proto.Clone(req).(*telemetrypb.FleetNodeTelemetryRequest)
			change(replacement)
			result, err := fetcher.Fetch(t.Context(), replacement)
			require.NoError(t, err)
			requirePhysicalTelemetryIdentity(t, result)
			require.EqualValues(t, 2, impl.created.Load())
			require.Eventually(t, func() bool { return impl.closed.Load() == 1 }, time.Second, time.Millisecond)
			fetcher.clearCachedDevices()
			require.Eventually(t, func() bool { return impl.closed.Load() == 2 }, time.Second, time.Millisecond)
		})
	}
}

func TestTelemetryReplacesChangedPlugin(t *testing.T) {
	first, second := &cleanupTestDriver{}, &cleanupTestDriver{}
	fetcher := newHandleTestTelemetryFetcher(t, newHandleTestGRPCDriver(t, first), newCleanupTestPool(t, 4))
	require.NoError(t, fetcher.manager.RegisterPluginForTest(&plugins.LoadedPlugin{
		Name: "other", Identifier: sdk.DriverIdentifier{DriverName: "other"},
		Driver: newHandleTestGRPCDriver(t, second), Caps: sdk.Capabilities{sdk.CapabilityRealtimeTelemetry: true},
	}))
	req := handleTestTelemetryRequest()
	_, err := fetcher.Fetch(t.Context(), req)
	require.NoError(t, err)
	// DriverName is not part of DeviceInfo: the credential/address key is unchanged.
	req.DriverName = "other"
	result, err := fetcher.Fetch(t.Context(), req)
	require.NoError(t, err)
	requirePhysicalTelemetryIdentity(t, result)
	require.EqualValues(t, 1, first.created.Load())
	require.EqualValues(t, 1, second.created.Load())
	require.Eventually(t, func() bool { return first.closed.Load() == 1 }, time.Second, time.Millisecond)
	fetcher.clearCachedDevices()
	require.Eventually(t, func() bool { return second.closed.Load() == 1 }, time.Second, time.Millisecond)
}

func TestTelemetryCacheExpiresWithoutPollingAndBoundsRetainedHandles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		impl := &cleanupTestDriver{}
		pool := newCleanupTestPool(t, maxOwnedDeviceHandles)
		fetcher := newHandleTestTelemetryFetcher(t, impl, pool)
		for i := range maxCachedTelemetryDevices + 1 {
			req := handleTestTelemetryRequest()
			req.DeviceIdentifier = fmt.Sprintf("dev-%d", i)
			_, err := fetcher.Fetch(t.Context(), req)
			require.NoError(t, err)
		}
		synctest.Wait()
		require.Len(t, pool.slots, maxCachedTelemetryDevices)
		require.EqualValues(t, 1, impl.closed.Load())
		// A full cache must leave the original active/cleanup allowance available,
		// including a slot for an operator command when other leases are retained.
		var retained []*deviceHandleLease
		for range 2*commandPoolSize - 1 {
			lease, err := pool.acquire()
			require.NoError(t, err)
			retained = append(retained, lease)
		}
		run := &RunCmd{driverGetter: fakeDriverGetter{d: impl}, minerSecrets: nodeSecretProvider{}, deviceHandles: pool}
		require.Equal(t, pb.AckCode_ACK_CODE_OK, runCleanupTestOperation(t, t.Context(), "command", run, fetcher))
		for _, lease := range retained {
			lease.release()
		}
		// Reuse must not extend the absolute lifetime.
		time.Sleep(45 * time.Second)
		_, err := fetcher.Fetch(t.Context(), handleTestTelemetryRequest())
		require.NoError(t, err)
		time.Sleep(16 * time.Second)
		synctest.Wait()
		require.Empty(t, pool.slots)
		require.Equal(t, impl.created.Load(), impl.closed.Load())
	})
}

type failingTelemetryDriver struct {
	cleanupTestDriver
	status func(context.Context) error
}

func (d *failingTelemetryDriver) NewDevice(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	d.created.Add(1)
	return sdk.NewDeviceResult{Device: &failingTelemetryDevice{cleanupTestDevice: cleanupTestDevice{id: id, owner: &d.cleanupTestDriver}, status: d.status}}, nil
}

type failingTelemetryDevice struct {
	cleanupTestDevice
	status func(context.Context) error
}

func (d *failingTelemetryDevice) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	if err := d.status(ctx); err != nil {
		return sdk.DeviceMetrics{}, err
	}
	return d.cleanupTestDevice.Status(ctx)
}

func TestTelemetryEvictsFailedAndAbandonedDevices(t *testing.T) {
	for _, mode := range []string{"auth failure", "canceled", "abandoned"} {
		t.Run(mode, func(t *testing.T) {
			impl := &failingTelemetryDriver{status: func(context.Context) error { return nil }}
			fetcher := newHandleTestTelemetryFetcher(t, impl, newCleanupTestPool(t, 4))
			_, err := fetcher.Fetch(t.Context(), handleTestTelemetryRequest())
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			// Simulate a failed or abandoned poll on the already cached device.
			fetcher.mu.Lock()
			entry := fetcher.cached["dev-1"]
			device := entry.device.(*failingTelemetryDevice)
			device.status = func(context.Context) error {
				switch mode {
				case "auth failure":
					return grpcstatus.Error(codes.Unauthenticated, "expired credentials")
				case "canceled":
					cancel()
				case "abandoned":
					require.True(t, fetcher.AbandonTelemetryFetch("dev-1"))
					_, duplicateErr := fetcher.Fetch(t.Context(), handleTestTelemetryRequest())
					require.ErrorContains(t, duplicateErr, "telemetry already in progress")
				}
				return nil
			}
			fetcher.mu.Unlock()
			_, _ = fetcher.Fetch(ctx, handleTestTelemetryRequest())
			require.Eventually(t, func() bool { return impl.closed.Load() == 1 }, time.Second, time.Millisecond)
			_, err = fetcher.Fetch(t.Context(), handleTestTelemetryRequest())
			require.NoError(t, err)
			require.EqualValues(t, 2, impl.created.Load())
			fetcher.clearCachedDevices()
			require.Eventually(t, func() bool { return impl.closed.Load() == 2 }, time.Second, time.Millisecond)
		})
	}
}
