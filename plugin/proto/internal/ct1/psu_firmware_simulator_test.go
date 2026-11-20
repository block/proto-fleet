package ct1

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// Runs a container-wide PSU update against the real simulator at the full
// 6x16 topology: one module fails programming and stops the update, and
// sending the capsule again recovers it and finishes the container.
func TestPSUFirmwareSimulatorUpdatesAndRecoversAContainer(t *testing.T) {
	const installed, target = "app=1310;bl=1110", "app=1410;bl=1110"
	binary := fakeCT1(t)
	ports := freePorts(t, 2)
	process := exec.CommandContext(t.Context(), binary)
	process.Env = append(os.Environ(), "HTTP_PORT="+ports[0], "GRPC_PORT="+ports[1], "SERVER_ID=psu-firmware-test", "CT1_DEMO_MODULE_STATES=false",
		"CT1_PSU_FIRMWARE_SPEED=200", "CT1_PSU_FIRMWARE_FAULT_MODULES=1/2")
	require.NoError(t, process.Start())
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	port := func(value string) int32 {
		parsed, err := strconv.ParseInt(value, 10, 32)
		require.NoError(t, err)
		return int32(parsed)
	}
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: port(ports[0]), RPCPort: port(ports[1]), URLScheme: "http", SerialNumber: "psu-firmware-test"}
	var dev *Device
	require.Eventually(t, func() bool {
		connected, err := NewDevice(t.Context(), "psu-firmware-test", info, sdk.UsernamePassword{})
		dev = connected
		return err == nil
	}, 10*time.Second, 50*time.Millisecond)
	t.Cleanup(func() { _ = dev.Close(t.Context()) })

	capsule, err := psucaptest.Build(psucaptest.Options{Version: target})
	require.NoError(t, err)
	// send runs one Fleet command to its end the way Fleet's worker does:
	// it polls, and sends the command again whenever the update reports that
	// a module is ready to start.
	send := func(requestID string) *sdk.FirmwareUpdateStatus {
		t.Helper()
		command := psuCommand{dev: dev, capsule: capsule, request: requestID}
		command.send(t)
		var current *sdk.FirmwareUpdateStatus
		require.Eventually(t, func() bool {
			current = command.poll(t)
			return current.State != firmwareStateInstalling && current.State != sdk.FirmwareStateContinue
		}, time.Minute, 5*time.Millisecond)
		return current
	}
	type board struct {
		PSUIdentity struct {
			FirmwareVersion string `json:"firmwareVersion"`
		} `json:"psuIdentity"`
		Update *psuFirmwareEvidence `json:"psuFirmwareUpdate"`
	}
	// module returns the PSU evidence every board of a module carries.
	module := func(metrics sdk.DeviceMetrics, tank, position int) (board, []sdk.ContainerHashboard) {
		t.Helper()
		hashboards := metrics.Container.Tanks[tank-1].Modules[position-1].Hashboards
		require.Len(t, hashboards, sdk.CT1HashboardsPerModule)
		var first board
		for index, hashboard := range hashboards {
			var decoded board
			require.NoError(t, json.Unmarshal(hashboard.MetadataJSON, &decoded))
			if index == 0 {
				first = decoded
			}
			require.Equal(t, first.PSUIdentity, decoded.PSUIdentity)
			require.Equal(t, first.Update == nil, decoded.Update == nil)
		}
		return first, hashboards
	}

	stopped := send("batch/1")
	require.Equal(t, firmwareStateFailed, stopped.State)
	require.Contains(t, *stopped.Error, "tank 1 module 2 (")
	require.Contains(t, *stopped.Error, "RECOVERY_REQUIRED")
	require.Contains(t, *stopped.Error, "update stopped because tank 1 module 2 was left out of mining; 94 modules were not attempted")
	metrics, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.Empty(t, metrics.Container.PSUFirmwareVersion)
	require.True(t, metrics.Container.PSUFirmwareMixed, "one updated module among modules still on the installed firmware")
	updated, boards := module(metrics, 1, 1)
	require.Equal(t, target, updated.PSUIdentity.FirmwareVersion)
	require.Equal(t, "PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED", updated.Update.State)
	require.InDelta(t, 100, updated.Update.Progress, 0)
	require.Equal(t, miningStateMining, boards[0].MiningState)
	failed, boards := module(metrics, 1, 2)
	require.Equal(t, installed, failed.PSUIdentity.FirmwareVersion)
	require.Equal(t, "PSU_FIRMWARE_UPDATE_STATE_FAILED", failed.Update.State)
	require.True(t, failed.Update.RecoveryRequired)
	require.True(t, failed.Update.MiningHeld)
	require.Equal(t, miningStateIdle, boards[0].MiningState, "a PSU that needs recovery keeps its module stopped")
	untouched, boards := module(metrics, 1, 3)
	require.Equal(t, installed, untouched.PSUIdentity.FirmwareVersion)
	require.Nil(t, untouched.Update)
	require.Equal(t, miningStateMining, boards[0].MiningState)

	require.Equal(t, firmwareStateCompleted, send("batch/2").State)
	metrics, err = dev.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, target, metrics.Container.PSUFirmwareVersion, "every PSU of the container reports the capsule's firmware")
	require.False(t, metrics.Container.PSUFirmwareMixed)
	tasks := map[[2]int]string{}
	for tank := 1; tank <= sdk.CT1TankCount; tank++ {
		for position := 1; position <= sdk.CT1ModulesPerTank; position++ {
			current, boards := module(metrics, tank, position)
			require.Equal(t, target, current.PSUIdentity.FirmwareVersion)
			require.Equal(t, "PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED", current.Update.State)
			require.Equal(t, tank == 1 && position == 2, current.Update.Recover, "only the failed module is recovered")
			require.Equal(t, miningStateMining, boards[0].MiningState)
			tasks[[2]int{tank, position}] = current.Update.TaskID
		}
	}
	require.Equal(t, updated.Update.TaskID, tasks[[2]int{1, 1}], "a PSU that still reports what its job installed gets no second job")

	// Sent a third time, every PSU still reports the capsule's firmware, so
	// no module gets another job.
	require.Equal(t, firmwareStateCompleted, send("batch/3").State)
	metrics, err = dev.Status(t.Context())
	require.NoError(t, err)
	for located, task := range tasks {
		current, _ := module(metrics, located[0], located[1])
		require.Equal(t, task, current.Update.TaskID, "tank %d module %d", located[0], located[1])
	}
}
