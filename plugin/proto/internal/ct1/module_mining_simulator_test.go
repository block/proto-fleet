package ct1

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	// Firmware convergence requires the full CT1 topology.
	process.Env = append(os.Environ(), "HTTP_PORT="+portText, "GRPC_PORT="+rpcPortText, "SERVER_ID=module-mining-test", "CT1_DEMO_MODULE_STATES=false")
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
	t.Run("mining groups", func(t *testing.T) { testMiningGroupsSimulator(t, dev) })
	t.Run("firmware", func(t *testing.T) { testFirmwareSimulator(t, dev, info) })
}

// Exercise the full native pool workflow on the same real simulator transport.
func testMiningGroupsSimulator(t *testing.T, dev *Device) {
	t.Helper()
	call := func(request sdk.MiningGroupRequest) sdk.MiningGroupResult {
		t.Helper()
		result, err := dev.MiningGroups(t.Context(), request)
		require.NoError(t, err)
		require.NoError(t, result.Validate(request))
		return result
	}
	observed := call(sdk.MiningGroupRequest{Action: "observe"})
	require.True(t, observed.Supported)
	require.False(t, observed.Enabled)
	initial := call(sdk.MiningGroupRequest{Action: "mining_status"})
	require.True(t, initial.MiningStatus.Mining)
	call(sdk.MiningGroupRequest{Action: "stop_legacy", Key: "stop"})
	stopped := call(sdk.MiningGroupRequest{Action: "mining_status"})
	require.False(t, stopped.MiningStatus.Mining)
	require.Zero(t, stopped.MiningStatus.ActiveBoards)
	call(sdk.MiningGroupRequest{Action: "activate", Revision: observed.Revision, Key: "activate"})
	observed = call(sdk.MiningGroupRequest{Action: "observe"})
	require.True(t, observed.Enabled)
	require.Len(t, observed.Groups, 1)
	source := observed.Groups[0]
	originalPools := source.Pools
	call(sdk.MiningGroupRequest{Action: "start", GroupID: "default", Revision: source.Revision, Key: "resume"})
	// A legacy stop replay after activation must leave resumed default mining alone.
	call(sdk.MiningGroupRequest{Action: "stop_legacy", Key: "stop"})
	resumed := call(sdk.MiningGroupRequest{Action: "observe"})
	require.True(t, resumed.Groups[0].Mining)
	require.Equal(t, originalPools, resumed.Groups[0].Pools)
	require.Positive(t, call(sdk.MiningGroupRequest{Action: "mining_status"}).MiningStatus.ActiveBoards)
	// Stop default for the existing exact-board assignment checks below.
	_, err := dev.clients[0].mining.StopMining(t.Context(), &mdk.StopMiningRequest{})
	require.NoError(t, err)
	source = call(sdk.MiningGroupRequest{Action: "observe"}).Groups[0]
	created := call(sdk.MiningGroupRequest{Action: "create", ConfigID: "customer", Key: "create"})
	require.False(t, created.Group.Mining)
	configured := call(sdk.MiningGroupRequest{Action: "replace_pools", GroupID: created.Group.ID, Revision: created.Group.Revision, Key: "pools", Pools: []sdk.MiningGroupPoolInput{
		{URL: "stratum+tcp://primary.example.com:3333", Username: "customer.worker", Password: "secret"},
		{URL: "stratum+tcp://backup.example.com:3333", Username: "customer.backup", Password: "backup-secret"},
	}})
	selected := []string{source.MACs[0], source.MACs[len(source.MACs)-1]}
	moved := call(sdk.MiningGroupRequest{Action: "transfer", SourceID: source.ID, SourceRevision: source.Revision, GroupID: created.Group.ID, Revision: configured.Group.Revision, MACs: selected, Key: "transfer"})
	require.Equal(t, "COMPLETE", moved.Operation.Phase)
	call(sdk.MiningGroupRequest{Action: "operation", GroupID: moved.Operation.ID})
	observed = call(sdk.MiningGroupRequest{Action: "observe"})
	for _, group := range observed.Groups {
		if group.ID != created.Group.ID {
			continue
		}
		require.Equal(t, selected, group.MACs)
		require.Len(t, group.Pools, 2)
		require.Equal(t, "customer.backup", group.Pools[1].Username)
		call(sdk.MiningGroupRequest{Action: "start", GroupID: group.ID, Revision: group.Revision, Key: "start"})
	}
	wire, err := dev.clients[0].boards.GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
	require.NoError(t, err)
	for _, tank := range mapResponse(wire).ContainerMap.Tanks {
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				if slot.Hashboard == nil {
					continue
				}
				want := miningStateIdle
				if strings.ToLower(slot.Hashboard.MACAddress) == selected[0] || strings.ToLower(slot.Hashboard.MACAddress) == selected[1] {
					want = miningStateMining
				}
				require.Equal(t, want, slot.Hashboard.MiningState, slot.Hashboard.MACAddress)
			}
		}
	}
}
