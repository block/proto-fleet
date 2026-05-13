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

	emitTerminalCommand(context.Background(), rec, int64(42), commandtype.Reboot, nil)
	emitTerminalCommand(context.Background(), rec, int64(7), commandtype.SetPowerTarget, errors.New("device offline"))

	require.Len(t, rec.events, 2)
	require.Equal(t, "reboot", rec.events[0].Kind)
	require.Equal(t, metrics.ResultSuccess, rec.events[0].Result)
	require.Equal(t, "42", rec.events[0].OrganizationID,
		"fleet_command_total must carry the owning org so org-scoped PromQL matches")
	require.Equal(t, "set_power_target", rec.events[1].Kind)
	require.Equal(t, metrics.ResultFailure, rec.events[1].Result)
	require.Equal(t, "7", rec.events[1].OrganizationID,
		"fleet_command_total must carry the owning org so org-scoped PromQL matches")
}

// TestEmitTerminalCommandDropsZeroOrgID makes sure org id 0 does not surface as a literal "0".
func TestEmitTerminalCommandDropsZeroOrgID(t *testing.T) {
	rec := &recordingCommandEmitter{}

	emitTerminalCommand(context.Background(), rec, int64(0), commandtype.Reboot, nil)

	require.Len(t, rec.events, 1)
	require.Empty(t, rec.events[0].OrganizationID)
}

// every commandtype.Type must produce a stable, lower_snake_case label string.
func TestCommandKindLabelCoversEveryCommandType(t *testing.T) {
	allTypes := []commandtype.Type{
		commandtype.StartMining,
		commandtype.StopMining,
		commandtype.SetCoolingMode,
		commandtype.SetPowerTarget,
		commandtype.UpdateMiningPools,
		commandtype.DownloadLogs,
		commandtype.Reboot,
		commandtype.BlinkLED,
		commandtype.FirmwareUpdate,
		commandtype.Unpair,
		commandtype.UpdateMinerPassword,
	}
	seen := make(map[string]struct{}, len(allTypes))
	for _, cmd := range allTypes {
		label := commandKindLabel(cmd)
		require.NotEmpty(t, label, "command type %v produced empty label", t)
		_, dup := seen[label]
		require.False(t, dup, "command type %v duplicates label %q", t, label)
		seen[label] = struct{}{}
	}
}

// emitTerminalCommand with the nop emitter must not panic.
func TestNoCommandMetricsIsSafeToCall(t *testing.T) {
	emitTerminalCommand(context.Background(), NoCommandMetrics(), int64(42), commandtype.Reboot, nil)
	emitTerminalCommand(context.Background(), nil, int64(42), commandtype.Reboot, nil)
}
