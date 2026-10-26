package driver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type containerServer struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedHashboardServiceServer
	mdk.UnimplementedMiningServiceServer
	mdk.UnimplementedTelemetryServiceServer
}

func (containerServer) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: "controller-a", Product: "Frigate", Version: "v1"}}, nil
}
func (containerServer) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	slots := []*mdk.Slot{
		{Occupied: true, Hashboard: &mdk.Hashboard{Serial: "board-a", Location: &mdk.HashboardLocation{HashboardPosition: new(uint32(1))}, Health: &mdk.HashboardHealth{Timestamp: timestamppb.Now(), Hashrate: 100, AverageAsicTemperatureC: new(65.0)}}},
		{ConfiguredLocation: &mdk.HashboardLocation{HashboardPosition: new(uint32(2))}},
		{ConfiguredLocation: &mdk.HashboardLocation{HashboardPosition: new(uint32(3))}},
	}
	return &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{Tanks: []*mdk.Tank{{Id: 1, Modules: []*mdk.Module{{Id: 1, Slots: slots}}}}}}, nil
}
func (containerServer) GetMiningStatus(context.Context, *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	return &mdk.GetMiningStatusResponse{Hashrate: 999, Mining: true}, nil
}
func (containerServer) GetContainerHealth(context.Context, *mdk.GetContainerHealthRequest) (*mdk.GetContainerHealthResponse, error) {
	return &mdk.GetContainerHealthResponse{Timestamp: timestamppb.Now(), Plant: &mdk.PlantHealth{TotalPowerW: new(9000.0), FacilityPowerW: new(3200.0), InletTemperatureC: new(25.0)}}, nil
}

func TestContainerLifecycle(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	fixture := containerServer{}
	mdk.RegisterSystemServiceServer(server, fixture)
	mdk.RegisterHashboardServiceServer(server, fixture)
	mdk.RegisterMiningServiceServer(server, fixture)
	mdk.RegisterTelemetryServiceServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	t.Setenv("CT1_WEB_PORT", "8181")
	driver, err := New(int(port))
	require.NoError(t, err)
	info, err := driver.DiscoverDevice(t.Context(), "127.0.0.1", portText)
	require.NoError(t, err)
	require.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
	require.Equal(t, int32(8181), info.Port)
	require.Equal(t, port, info.RPCPort)
	info, err = driver.PairDevice(t.Context(), info, sdk.SecretBundle{})
	require.NoError(t, err)
	restarted, err := New(int(port))
	require.NoError(t, err)
	result, err := restarted.NewDevice(t.Context(), "container-1", info, sdk.SecretBundle{})
	require.NoError(t, err)
	defer result.Device.Close(t.Context())
	metrics, err := result.Device.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, "container-1", metrics.DeviceID)
	require.Equal(t, "v1", metrics.FirmwareVersion)
	require.NotNil(t, metrics.HashrateHS)
	require.Equal(t, 100e12, metrics.HashrateHS.Value)
	require.NotNil(t, metrics.PowerW)
	require.Equal(t, 3200.0, metrics.PowerW.Value)
	require.NotNil(t, metrics.TempC)
	require.Equal(t, 65.0, metrics.TempC.Value)
	require.Equal(t, 25.0, *metrics.Container.Plant.InletTemperatureC)
	require.Nil(t, metrics.EfficiencyJH, "the partial container map cannot establish full coverage")
	require.Equal(t, sdk.MinerStatusUnknown, metrics.Container.Tanks[0].Modules[0].State)
	web, ok, err := result.Device.TryGetWebViewURL(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "http://127.0.0.1:8181", web)
	caps := restarted.GetCapabilitiesForModel(t.Context(), info.Manufacturer, info.Model)
	require.True(t, caps[sdk.CapabilityRealtimeTelemetry])
	require.False(t, caps[sdk.CapabilityMiningStop])
	require.False(t, caps[sdk.CapabilityBasicAuth])
	info.SerialNumber = "another-controller"
	_, err = restarted.NewDevice(t.Context(), "wrong", info, sdk.SecretBundle{})
	require.ErrorContains(t, err, "identity mismatch")
	_, err = driver.PairDevice(t.Context(), info, sdk.SecretBundle{})
	require.ErrorContains(t, err, "identity mismatch")
}

func TestContainerDiscoveryRejectsConfiguredBearerToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("test-token\n"), 0o600))
	t.Setenv("CT1_RPC_TOKEN_FILE", path)
	driver, err := New(0)
	require.NoError(t, err)
	_, err = driver.DiscoverDevice(t.Context(), "127.0.0.1", "9095")
	var sdkErr sdk.SDKError
	require.ErrorAs(t, err, &sdkErr)
	require.Equal(t, sdk.ErrCodeInvalidConfig, sdkErr.Code)
	require.ErrorContains(t, err, "CT1_RPC_TOKEN_FILE is unsupported")
	require.NotContains(t, err.Error(), "test-token")
}

type slowContainerIdentityServer struct {
	containerServer
	mdk.UnimplementedServerUpdateServiceServer
}

func (slowContainerIdentityServer) GetSystemInfo(ctx context.Context, _ *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("system info delay: %w", ctx.Err())
	case <-time.After(600 * time.Millisecond):
	}
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: "controller-a", Product: "Frigate"}}, nil
}

func (slowContainerIdentityServer) GetServerUpdateSupport(ctx context.Context, _ *mdk.GetServerUpdateSupportRequest) (*mdk.GetServerUpdateSupportResponse, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("partner info delay: %w", ctx.Err())
	case <-time.After(600 * time.Millisecond):
	}
	return &mdk.GetServerUpdateSupportResponse{InstanceId: "controller-a", PeerInstanceId: "controller-b"}, nil
}

func TestContainerDiscoveryAllowsSlowSequentialIdentityRPCs(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	fixture := slowContainerIdentityServer{}
	mdk.RegisterSystemServiceServer(server, fixture)
	mdk.RegisterServerUpdateServiceServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	driver, err := New(int(port))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	info, err := driver.DiscoverDevice(ctx, host, portText)
	require.NoError(t, err)
	require.Equal(t, "controller-a", info.SerialNumber)
	require.Len(t, info.Endpoints, 1)
	require.Equal(t, []string{"controller-a", "controller-b"}, info.Endpoints[0].MemberIDs)

	shortCtx, shortCancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer shortCancel()
	_, err = discoverContainer(shortCtx, host, port)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestContainerDiscoveryBoundsWebPortFallback(t *testing.T) {
	probeStarted := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			select {
			case probeStarted <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	}))
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetHTTP1(true)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	driver, err := New(0)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	_, err = driver.DiscoverDevice(ctx, host, port)
	var sdkErr sdk.SDKError
	require.ErrorAs(t, err, &sdkErr)
	require.Equal(t, sdk.ErrCodeDeviceNotFound, sdkErr.Code)
	require.NoError(t, ctx.Err(), "the stalled fallback must not consume the caller's full budget")
	select {
	case <-probeStarted:
	default:
		t.Fatal("the CT1 fallback did not reach the stalled HTTP/2 handler")
	}
}
