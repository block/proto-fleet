package ct1

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// Build server/fake-ct1 and supply its path to exercise the real stateful
// controller fixture. Each test owns a separate process and ephemeral port.
func TestModuleMiningSimulatorLeavesNeighborsUnaffected(t *testing.T) {
	binary := os.Getenv("CT1_SIMULATOR_BIN")
	if binary == "" {
		t.Skip("set CT1_SIMULATOR_BIN to the built fake-ct1 binary")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.ParseInt(portText, 10, 32)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	rpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, rpcPortText, err := net.SplitHostPort(rpcListener.Addr().String())
	require.NoError(t, err)
	rpcPort, err := strconv.ParseInt(rpcPortText, 10, 32)
	require.NoError(t, err)
	require.NoError(t, rpcListener.Close())
	process := exec.CommandContext(t.Context(), binary)
	process.Env = append(os.Environ(), "HTTP_PORT="+portText, "GRPC_PORT="+rpcPortText, "SERVER_ID=module-mining-test", "CT1_TANKS=2", "CT1_MODULES_PER_TANK=2", "CT1_DEMO_MODULE_STATES=false")
	require.NoError(t, process.Start())
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: int32(port), RPCPort: int32(rpcPort), URLScheme: "http", SerialNumber: "module-mining-test"}
	probe, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = probe.Close() })
	var before MapResponse
	require.Eventually(t, func() bool {
		wire, readErr := probe.boards.GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
		if readErr != nil {
			return false
		}
		before = mapResponse(wire)
		return before.ContainerMap != nil
	}, 10*time.Second, 50*time.Millisecond)
	dev, err := NewDevice(t.Context(), "module-test", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(t.Context()) })
	req := sdk.ModuleMiningRequest{Mining: new(false)}
	expected := map[string]bool{}
	neighborStates := map[string]string{}
	for _, tank := range before.ContainerMap.Tanks {
		for _, module := range tank.Modules {
			selected := (tank.ID == 1 && module.ID == 1) || (tank.ID == 2 && module.ID == 2)
			if selected {
				require.NotEmpty(t, module.Slots[0].Hashboard.ModuleID)
				req.Modules = append(req.Modules, sdk.ModuleMiningTarget{Tank: tank.ID, Module: module.ID, ReportedID: module.Slots[0].Hashboard.ModuleID})
			}
			for _, slot := range module.Slots {
				if slot.Hashboard != nil {
					if selected {
						expected[slot.Hashboard.MACAddress] = true
					} else {
						neighborStates[slot.Hashboard.MACAddress] = slot.Hashboard.MiningState
					}
				}
			}
		}
	}
	require.Len(t, req.Modules, 2)
	require.Len(t, expected, 6)
	for _, mining := range []bool{false, true} {
		req.Mining = &mining
		require.NoError(t, dev.SetModuleMining(t.Context(), req))
		wire, err := dev.clients[0].boards.GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
		require.NoError(t, err)
		after := mapResponse(wire)
		for _, tank := range after.ContainerMap.Tanks {
			for _, module := range tank.Modules {
				for _, slot := range module.Slots {
					if slot.Hashboard == nil {
						continue
					}
					b := slot.Hashboard
					want := neighborStates[b.MACAddress]
					if expected[b.MACAddress] {
						want = miningStateIdle
						if mining {
							want = miningStateMining
						}
					}
					require.Equal(t, want, b.MiningState, fmt.Sprintf("tank %d module %d board %s", tank.ID, module.ID, b.MACAddress))
				}
			}
		}
	}
}
