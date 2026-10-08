package sdk

import (
	"context"
	"encoding/json"
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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// storedPoolDevice retains exact write credentials while exposing a secret-free
// runtime pool view.
type storedPoolDevice struct {
	fakeDevice
	mu    sync.Mutex
	pools []MiningPoolConfig
}

func (d *storedPoolDevice) UpdateMiningPools(_ context.Context, pools []MiningPoolConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pools = append([]MiningPoolConfig(nil), pools...)
	return nil
}

func (d *storedPoolDevice) GetMiningPools(context.Context) ([]ConfiguredPool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	pools := make([]ConfiguredPool, len(d.pools))
	for i, pool := range d.pools {
		pools[i] = ConfiguredPool{Priority: pool.Priority, URL: pool.URL, Username: pool.WorkerName}
	}
	return pools, nil
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

	encoded, err := json.Marshal(readback)
	require.NoError(t, err)
	for _, pool := range pools[:2] {
		require.NotContains(t, string(encoded), pool.Password, "ordinary pool readback must never contain credentials")
	}
	require.Nil(t, (&pb.ConfiguredPool{}).ProtoReflect().Descriptor().Fields().ByName("password"), "ordinary readback must not have a password field on the wire")

}

func TestMiningPoolCredentialsRedactedWhenFormattedAndLogged(t *testing.T) {
	password := "secret-must-not-be-logged"
	values := []any{
		MiningPoolConfig{Priority: 0, URL: "stratum+tcp://pool.example:3333", WorkerName: "worker", Password: password},
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
