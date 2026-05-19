package command

import (
	"context"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

type MetricsEmitter interface {
	EmitCommand(ctx context.Context, labels metrics.CommandLabels)
}

type nopCommandMetrics struct{}

func (nopCommandMetrics) EmitCommand(context.Context, metrics.CommandLabels) {}

func NoCommandMetrics() MetricsEmitter { return nopCommandMetrics{} }

func commandKindLabel(t commandtype.Type) string {
	switch t {
	case commandtype.StartMining:
		return "start_mining"
	case commandtype.StopMining:
		return "stop_mining"
	case commandtype.SetCoolingMode:
		return "set_cooling_mode"
	case commandtype.SetPowerTarget:
		return "set_power_target"
	case commandtype.UpdateMiningPools:
		return "update_mining_pools"
	case commandtype.DownloadLogs:
		return "download_logs"
	case commandtype.Reboot:
		return "reboot"
	case commandtype.BlinkLED:
		return "blink_led"
	case commandtype.FirmwareUpdate:
		return "firmware_update"
	case commandtype.Unpair:
		return "unpair"
	case commandtype.UpdateMinerPassword:
		return "update_miner_password"
	case commandtype.Curtail:
		return "curtail"
	case commandtype.Uncurtail:
		return "uncurtail"
	default:
		return strings.ToLower((&t).String())
	}
}

func emitTerminalCommand(ctx context.Context, emitter MetricsEmitter, orgID int64, kind commandtype.Type, workerError error) {
	if emitter == nil {
		return
	}
	result := metrics.ResultSuccess
	if workerError != nil {
		result = metrics.ResultFailure
	}
	emitter.EmitCommand(ctx, metrics.CommandLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		Kind:           commandKindLabel(kind),
		Result:         result,
	})
}
