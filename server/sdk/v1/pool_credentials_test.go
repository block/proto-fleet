package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// storedPoolDevice simulates firmware storage, with password visibility
// independently controlled from the values accepted by UpdateMiningPools.
type storedPoolDevice struct {
	fakeDevice
	mu               sync.Mutex
	pools            []MiningPoolConfig
	updateCalls      int
	hiddenPasswords  map[int32]bool
	storedReadError  error
	runtimeReadError error
}

func (d *storedPoolDevice) UpdateMiningPools(_ context.Context, pools []MiningPoolConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.updateCalls++
	d.pools = append([]MiningPoolConfig(nil), pools...)
	return nil
}

func (d *storedPoolDevice) GetMiningPools(context.Context) ([]ConfiguredPool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.runtimeReadError != nil {
		return nil, d.runtimeReadError
	}
	pools := make([]ConfiguredPool, len(d.pools))
	for i, p := range d.pools {
		pools[i] = ConfiguredPool{Priority: p.Priority, URL: p.URL, Username: p.WorkerName}
	}
	return pools, nil
}

func (d *storedPoolDevice) storedPoolsLocked() []StoredMiningPool {
	pools := make([]StoredMiningPool, len(d.pools))
	for i, pool := range d.pools {
		pools[i] = StoredMiningPool{Priority: pool.Priority, URL: pool.URL, Username: pool.WorkerName}
		if !d.hiddenPasswords[pool.Priority] {
			pools[i].Password = ptr(pool.Password)
		}
	}
	return pools
}

func (d *storedPoolDevice) GetStoredMiningPools(_ context.Context) ([]StoredMiningPool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.storedReadError != nil {
		return nil, d.storedReadError
	}
	return d.storedPoolsLocked(), nil
}

func TestMiningPoolCredentialsGRPCTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	device := &storedPoolDevice{}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterDriverServer(server, &DriverGRPCServer{
		Impl: fakeDriver{newDeviceFunc: func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
			return NewDeviceResult{Device: device}, nil
		}},
		devices: make(map[string]Device),
	})
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		assert.NoError(t, listener.Close())
		assert.NoError(t, <-serveDone)
	})
	conn, err := grpc.NewClient("passthrough:///pool-credentials-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	driver := &DriverGRPCClient{client: pb.NewDriverClient(conn)}
	created, err := driver.NewDevice(ctx, "device-123", DeviceInfo{}, SecretBundle{})
	require.NoError(t, err)
	client := created.Device

	pools := []MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://primary.example.com:3333", WorkerName: "primary.worker", Password: "primary-password"},
		{Priority: 1, URL: "stratum+tcp://backup1.example.com:3333", WorkerName: "backup1.worker", Password: "backup-password"},
		{Priority: 2, URL: "stratum+tcp://backup2.example.com:3333", WorkerName: "backup2.worker", Password: ""},
	}
	// A raw wire caller must provide every password, even when a preceding pool
	// is valid. Reject the whole request before invoking the device.
	provided := "must-not-appear-in-errors"
	for _, invalidPool := range []*pb.MiningPool{
		{Priority: 1, Url: "stratum+tcp://backup.example:3333", WorkerName: "worker"},
		nil,
	} {
		_, err := pb.NewDriverClient(conn).UpdateMiningPools(ctx, &pb.UpdateMiningPoolsRequest{
			Ref: &pb.DeviceRef{DeviceId: "device-123"},
			Pools: []*pb.MiningPool{
				{Priority: 0, Url: pools[0].URL, WorkerName: pools[0].WorkerName, Password: &provided},
				invalidPool,
			},
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.NotContains(t, err.Error(), provided)
		device.mu.Lock()
		updateCalls := device.updateCalls
		device.mu.Unlock()
		require.Zero(t, updateCalls, "an omitted password must reject the entire write")
	}

	require.NoError(t, client.UpdateMiningPools(ctx, pools))
	device.mu.Lock()
	stored := append([]MiningPoolConfig(nil), device.pools...)
	device.mu.Unlock()
	require.Equal(t, pools, stored, "the actual device must receive every write credential, including empty")

	readback, err := client.GetMiningPools(ctx)
	require.NoError(t, err)
	require.Len(t, readback, len(pools))
	for i, pool := range pools {
		assert.Equal(t, pool.Priority, readback[i].Priority)
		assert.Equal(t, pool.URL, readback[i].URL)
		assert.Equal(t, pool.WorkerName, readback[i].Username)
	}

	storedReader, ok := client.(DeviceStoredPoolReader)
	require.True(t, ok)
	storedReadback, err := storedReader.GetStoredMiningPools(ctx)
	require.NoError(t, err)
	require.Len(t, storedReadback, len(pools))
	for i, pool := range pools {
		require.NotNil(t, storedReadback[i].Password)
		require.Equal(t, pool.Password, *storedReadback[i].Password)
	}
	encoded, err := json.Marshal(readback)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), pools[0].Password)
	require.NotContains(t, string(encoded), pools[1].Password)
	require.Nil(t, (&pb.ConfiguredPool{}).ProtoReflect().Descriptor().Fields().ByName("password"))

	// Firmware may withhold a password or report a changed stored value.
	// Neither result may be replaced with the last desired write credential.
	device.mu.Lock()
	device.hiddenPasswords = map[int32]bool{0: true}
	device.pools[1].Password = "firmware-changed-password"
	device.mu.Unlock()
	changed, err := storedReader.GetStoredMiningPools(ctx)
	require.NoError(t, err)
	require.Len(t, changed, 3)
	assert.Nil(t, changed[0].Password, "unproven readback must remain absent")
	require.NotNil(t, changed[1].Password)
	assert.Equal(t, "firmware-changed-password", *changed[1].Password)
	require.NotNil(t, changed[2].Password, "explicit stored empty password must remain present")
	assert.Empty(t, *changed[2].Password)
	device.mu.Lock()
	device.runtimeReadError = errors.New("runtime telemetry unavailable")
	device.mu.Unlock()
	_, err = client.GetMiningPools(ctx)
	require.Error(t, err)
	storedReadback, err = storedReader.GetStoredMiningPools(ctx)
	require.NoError(t, err)
	require.Equal(t, changed, storedReadback, "stored reads must remain actual observations")
	serverImpl := &DriverGRPCServer{devices: map[string]Device{"legacy": fakeDevice{}}}
	_, err = serverImpl.GetStoredMiningPools(ctx, &pb.GetStoredMiningPoolsRequest{Ref: &pb.DeviceRef{DeviceId: "legacy"}})
	require.Equal(t, codes.Unimplemented, status.Code(err), "runtime telemetry cannot qualify stored readback")
}

func TestMiningPoolCredentialsRedactedWhenFormattedAndLogged(t *testing.T) {
	password := "secret-must-not-be-logged"
	values := []any{
		MiningPoolConfig{Priority: 0, URL: "stratum+tcp://pool.example:3333", WorkerName: "worker", Password: password},
		StoredMiningPool{Priority: 0, URL: "stratum+tcp://pool.example:3333", Username: "worker", Password: &password},
	}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			assert.NotContains(t, fmt.Sprintf(format, value), password)
		}
		var output strings.Builder
		slog.New(slog.NewJSONHandler(&output, nil)).Info("pool", "config", value)
		assert.NotContains(t, output.String(), password)
	}
}

func TestStoredPoolReadRejectsMissingReferencesAndRedactsFirmwareErrors(t *testing.T) {
	const secret = "firmware-private-pool-password"
	device := &storedPoolDevice{storedReadError: errors.New("echo " + secret)}
	server := &DriverGRPCServer{devices: map[string]Device{"device": device}}
	for _, req := range []*pb.GetStoredMiningPoolsRequest{nil, {}, {Ref: &pb.DeviceRef{}}} {
		_, err := server.GetStoredMiningPools(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	_, err := server.GetStoredMiningPools(t.Context(), &pb.GetStoredMiningPoolsRequest{Ref: &pb.DeviceRef{DeviceId: "missing"}})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = server.GetStoredMiningPools(t.Context(), &pb.GetStoredMiningPoolsRequest{Ref: &pb.DeviceRef{DeviceId: "device"}})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), secret)
}

func TestStoredPoolReadPreservesUnsupportedClassification(t *testing.T) {
	const firmwareEcho = "firmware echoed a private configuration value"
	for _, unsupported := range []error{NewErrUnsupportedCapability(firmwareEcho), status.Error(codes.Unimplemented, firmwareEcho)} {
		t.Run(fmt.Sprintf("%T", unsupported), func(t *testing.T) {
			device := &storedPoolDevice{storedReadError: unsupported}
			server := &DriverGRPCServer{devices: map[string]Device{"device": device}}
			_, err := server.GetStoredMiningPools(t.Context(), &pb.GetStoredMiningPoolsRequest{Ref: &pb.DeviceRef{DeviceId: "device"}})
			require.Equal(t, codes.Unimplemented, status.Code(err))
			require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), firmwareEcho)
		})
	}
}
