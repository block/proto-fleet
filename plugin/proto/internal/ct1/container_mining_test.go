package ct1

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type nativeMiningFixture struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedMiningServiceServer
	identity atomic.Value
	writes   atomic.Int32
	fail     bool
	power    *mdk.SetTargetPowerRequest
}

func (f *nativeMiningFixture) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	id, _ := f.identity.Load().(string)
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: id, Model: "CT1"}}, nil
}
func (f *nativeMiningFixture) GetMiningStatus(context.Context, *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	return &mdk.GetMiningStatusResponse{Mining: true, OperatingState: mdk.MiningOperatingState_MINING_OPERATING_STATE_MINING}, nil
}
func (f *nativeMiningFixture) GetTargetPower(context.Context, *mdk.GetTargetPowerRequest) (*mdk.GetTargetPowerResponse, error) {
	return &mdk.GetTargetPowerResponse{Watts: 3000, MinimumWatts: new(1000.0), MaximumWatts: new(5000.0), PerformanceMode: mdk.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY, BalanceBays: new(false), HashOnDisconnect: new(true)}, nil
}
func (f *nativeMiningFixture) write() error {
	f.writes.Add(1)
	if f.fail {
		return fmt.Errorf("fixture: %w", status.Error(codes.Unavailable, "ack lost"))
	}
	return nil
}
func (f *nativeMiningFixture) StartMining(context.Context, *mdk.StartMiningRequest) (*mdk.StartMiningResponse, error) {
	return &mdk.StartMiningResponse{}, f.write()
}
func (f *nativeMiningFixture) StopMining(context.Context, *mdk.StopMiningRequest) (*mdk.StopMiningResponse, error) {
	return &mdk.StopMiningResponse{}, f.write()
}
func (f *nativeMiningFixture) SetTargetPower(_ context.Context, r *mdk.SetTargetPowerRequest) (*mdk.SetTargetPowerResponse, error) {
	f.power = r
	return &mdk.SetTargetPowerResponse{Watts: r.Watts}, f.write()
}
func newNativeMiningFixture(t *testing.T) (*Device, *nativeMiningFixture) {
	t.Helper()
	f := &nativeMiningFixture{}
	f.identity.Store("ct1")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	mdk.RegisterSystemServiceServer(server, f)
	mdk.RegisterMiningServiceServer(server, f)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	d, err := NewDevice(t.Context(), "ct1", sdk.DeviceInfo{Host: host, Port: 8080, RPCPort: port, URLScheme: "http", SerialNumber: "ct1"}, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, d.Close(context.Background())) })
	return d, f
}
func TestContainerMiningUsesNativeCommandsAndPreservesOptions(t *testing.T) {
	d, f := newNativeMiningFixture(t)
	result, err := d.ContainerMining(t.Context(), sdk.ContainerMiningRequest{Action: "status"})
	require.NoError(t, err)
	require.Equal(t, new(true), result.Status.Mining)
	require.Equal(t, "MINING_OPERATING_STATE_MINING", result.Status.OperatingState)
	result, err = d.ContainerMining(t.Context(), sdk.ContainerMiningRequest{Action: "power_target"})
	require.NoError(t, err)
	require.Equal(t, new(3000.0), result.Power.Watts)
	require.Equal(t, new(1000.0), result.Power.MinimumWatts)
	require.Equal(t, new(false), result.Power.BalanceBays)
	for _, mining := range []bool{false, true} {
		result, err = d.ContainerMining(t.Context(), sdk.ContainerMiningRequest{Action: "set_mining", Mining: &mining})
		require.NoError(t, err)
		require.Equal(t, sdk.ContainerMiningResult{}, result)
	}
	result, err = d.ContainerMining(t.Context(), sdk.ContainerMiningRequest{Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 2500, PerformanceMode: "PERFORMANCE_MODE_EFFICIENCY", BalanceBays: new(false), HashOnDisconnect: new(true)}})
	require.NoError(t, err)
	require.Equal(t, sdk.ContainerMiningResult{}, result)
	require.Equal(t, 2500.0, f.power.Watts)
	require.Equal(t, mdk.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY, f.power.PerformanceMode)
	require.Equal(t, new(false), f.power.BalanceBays)
	require.Equal(t, new(true), f.power.HashOnDisconnect)
	require.EqualValues(t, 3, f.writes.Load())
}
func TestContainerMiningRejectsChangedIdentityAndDoesNotReplay(t *testing.T) {
	for _, action := range []sdk.ContainerMiningRequest{{Action: "status"}, {Action: "power_target"}, {Action: "set_mining", Mining: new(false)}, {Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 2500}}} {
		t.Run(action.Action, func(t *testing.T) {
			d, f := newNativeMiningFixture(t)
			f.identity.Store("replacement")
			_, err := d.ContainerMining(t.Context(), action)
			require.Error(t, err)
			require.Zero(t, f.writes.Load())
		})
	}
	d, f := newNativeMiningFixture(t)
	f.fail = true
	_, err := d.ContainerMining(t.Context(), sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)})
	require.Error(t, err)
	require.EqualValues(t, 1, f.writes.Load())
}

func (s *haServer) StopMining(context.Context, *mdk.StopMiningRequest) (*mdk.StopMiningResponse, error) {
	s.writes.Add(1)
	if s.uncertain.Load() {
		return nil, fmt.Errorf("simulated mining write: %w", status.Error(codes.Unavailable, "ack lost"))
	}
	return &mdk.StopMiningResponse{}, nil
}
func TestContainerMiningUsesVerifiedHAPrimaryWithoutReplay(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	b.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, d.Close(context.Background())) })
	req := sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}
	_, err = d.ContainerMining(t.Context(), req)
	require.NoError(t, err)
	require.Zero(t, a.writes.Load())
	require.EqualValues(t, 1, b.writes.Load())
	b.uncertain.Store(true)
	_, err = d.ContainerMining(t.Context(), req)
	require.Error(t, err)
	require.Zero(t, a.writes.Load())
	require.EqualValues(t, 2, b.writes.Load())
}
