package command

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

type recordingCommandEmitter struct {
	mu     sync.Mutex
	events []metrics.CommandLabels
}

func (r *recordingCommandEmitter) EmitCommand(_ context.Context, labels metrics.CommandLabels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, labels)
}

// TestEmitTerminalCommandSuccessAndFailure exercises both branches of the fleet_command_total result enum
func TestEmitTerminalCommandSuccessAndFailure(t *testing.T) {
	rec := &recordingCommandEmitter{}

	emitTerminalCommand(context.Background(), rec, int64(42), int64(99), commandtype.Reboot, nil)
	emitTerminalCommand(context.Background(), rec, int64(7), int64(0), commandtype.SetPowerTarget, errors.New("device offline"))

	require.Len(t, rec.events, 2)
	require.Equal(t, "reboot", rec.events[0].Kind)
	require.Equal(t, metrics.ResultSuccess, rec.events[0].Result)
	require.Equal(t, "42", rec.events[0].OrganizationID,
		"fleet_command_total must carry the owning org so org-scoped PromQL matches")
	require.Equal(t, "99", rec.events[0].SiteID,
		"fleet_command_total must carry the device's site so site-scoped PromQL matches")
	require.Equal(t, "set_power_target", rec.events[1].Kind)
	require.Equal(t, metrics.ResultFailure, rec.events[1].Result)
	require.Equal(t, "7", rec.events[1].OrganizationID,
		"fleet_command_total must carry the owning org so org-scoped PromQL matches")
}

// TestEmitTerminalCommandDropsZeroOrgID makes sure org id 0 does not surface as a literal "0".
func TestEmitTerminalCommandDropsZeroOrgID(t *testing.T) {
	rec := &recordingCommandEmitter{}

	emitTerminalCommand(context.Background(), rec, int64(0), int64(0), commandtype.Reboot, nil)

	require.Len(t, rec.events, 1)
	require.Empty(t, rec.events[0].OrganizationID)
	require.Empty(t, rec.events[0].SiteID)
}

// The kind label of fleet_command_total is a metric contract. Each
// commandtype.Type has one fixed lower_snake_case label.
func TestCommandKindLabelCoversEveryCommandType(t *testing.T) {
	labels := map[commandtype.Type]string{
		commandtype.StartMining:            "start_mining",
		commandtype.StopMining:             "stop_mining",
		commandtype.SetCoolingMode:         "set_cooling_mode",
		commandtype.SetPowerTarget:         "set_power_target",
		commandtype.UpdateMiningPools:      "update_mining_pools",
		commandtype.DownloadLogs:           "download_logs",
		commandtype.Reboot:                 "reboot",
		commandtype.BlinkLED:               "blink_led",
		commandtype.FirmwareUpdate:         "firmware_update",
		commandtype.Unpair:                 "unpair",
		commandtype.UpdateMinerPassword:    "update_miner_password",
		commandtype.Curtail:                "curtail",
		commandtype.Uncurtail:              "uncurtail",
		commandtype.ApplyCurtailmentConfig: "apply_curtailment_config",
		commandtype.SetContainerControl:    "set_container_control",
		commandtype.SetTankMining:          "set_tank_mining",
		commandtype.SetModuleMining:        "set_module_mining",
		commandtype.ContainerMining:        "container_mining",
		commandtype.SetBoardIdentify:       "set_board_identify",
		commandtype.FrigateServerUpdate:    "frigate_server_update",
		commandtype.ContainerAlarmAction:   "container_alarm_action",
		commandtype.ControlPowerFeed:       "control_power_feed",
	}

	// String returns "Undefined" for the first value after the last declared type.
	declared := 0
	for cmd := commandtype.StartMining; cmd.String() != "Undefined"; cmd++ {
		declared++
		want, ok := labels[cmd]
		require.True(t, ok, "command type %s has no expected label in this table", cmd.String())
		require.Equal(t, want, commandKindLabel(cmd), "label of command type %s", cmd.String())
	}
	require.Equal(t, len(labels), declared, "the table must have one row for each declared command type")
}

// emitTerminalCommand with the nop emitter must not panic.
func TestNoCommandMetricsIsSafeToCall(t *testing.T) {
	emitTerminalCommand(context.Background(), NoCommandMetrics(), int64(42), int64(0), commandtype.Reboot, nil)
	emitTerminalCommand(context.Background(), nil, int64(42), int64(0), commandtype.Reboot, nil)
}

// TestEmitReapedCommandMetricsEmitsFailurePerRow verifies the reaper path
// produces a result="failure" fleet_command_total sample per reaped row.
func TestEmitReapedCommandMetricsEmitsFailurePerRow(t *testing.T) {
	rec := &recordingCommandEmitter{}
	es := &ExecutionService{metricsEmitter: rec}

	es.emitReapedCommandMetrics(context.Background(), []reapedCommand{
		{orgID: 11, commandType: "Reboot"},
		{orgID: 22, commandType: "FirmwareUpdate"},
		{orgID: 0, commandType: "SetPowerTarget"},
	})

	require.Len(t, rec.events, 3)
	for i, ev := range rec.events {
		require.Equal(t, metrics.ResultFailure, ev.Result,
			"reaper must emit result=failure (event %d)", i)
	}
	require.Equal(t, "reboot", rec.events[0].Kind)
	require.Equal(t, "11", rec.events[0].OrganizationID)
	require.Equal(t, "firmware_update", rec.events[1].Kind)
	require.Equal(t, "22", rec.events[1].OrganizationID)
	require.Equal(t, "set_power_target", rec.events[2].Kind)
	require.Empty(t, rec.events[2].OrganizationID,
		"org id 0 must not surface as a literal label")
}

// TestEmitReapedCommandMetricsSkipsUnknownType ensures a future code version
// sending a command_type the binary doesn't know about doesn't crash the reaper.
func TestEmitReapedCommandMetricsSkipsUnknownType(t *testing.T) {
	rec := &recordingCommandEmitter{}
	es := &ExecutionService{metricsEmitter: rec}

	es.emitReapedCommandMetrics(context.Background(), []reapedCommand{
		{orgID: 1, commandType: "Reboot"},
		{orgID: 2, commandType: "NotARealCommand"},
	})

	require.Len(t, rec.events, 1)
	require.Equal(t, "reboot", rec.events[0].Kind)
}

// an ExecutionService that never had a metrics emitter installed must still tolerate the reaper running.
func TestEmitReapedCommandMetricsNilEmitterIsSafe(t *testing.T) {
	es := &ExecutionService{metricsEmitter: nil}
	es.emitReapedCommandMetrics(context.Background(), []reapedCommand{
		{orgID: 1, commandType: "Reboot"},
	})

	esNop := &ExecutionService{metricsEmitter: NoCommandMetrics()}
	esNop.emitReapedCommandMetrics(context.Background(), []reapedCommand{
		{orgID: 1, commandType: "Reboot"},
	})
}
