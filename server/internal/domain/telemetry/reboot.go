package telemetry

import (
	"context"
	"log/slog"
	"time"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const rebootReconcileTimeout = 10 * time.Second

func (s *TelemetryService) reconcileRebootRequiredState(ctx context.Context, deviceID models.DeviceIdentifier, metrics modelsV2.DeviceMetrics, status mm.MinerStatus) {
	bootedAfter, ok := metrics.BootTimeLowerBound(time.Now())
	if !ok {
		return
	}
	// Check devices without a cached status hint as well as pending firmware
	// states. Failed status refinement invalidates the hint so uptime can retry.
	if value, seen := s.firmwareStatusPending.Load(deviceID); seen {
		if pending, _ := value.(bool); !pending {
			return
		}
	}
	if previous, seen := s.lastReconciledBoot.Load(deviceID); seen {
		if previousBound, valid := previous.(time.Time); valid && !bootedAfter.After(previousBound) {
			return
		}
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, rebootReconcileTimeout)
	defer cancel()
	// Compare the boot evidence to the current reboot boundary in the UPDATE
	// itself. A newer installation must win even if it races this observation.
	updated, err := s.deviceStore.ReconcileRebootRequiredStatus(reconcileCtx, deviceID, bootedAfter, status)
	if err != nil {
		slog.Warn("failed to reconcile reboot-required status", "device_id", deviceID, "error", err)
		return // Retry the observation on the next successful telemetry poll.
	}
	if updated {
		// A sample first seen while UPDATING can later satisfy a pending reboot
		// recorded after that boot. Do not cache a query that matched no row.
		s.lastReconciledBoot.Store(deviceID, bootedAfter)
		s.firmwareStatusPending.Store(deviceID, false)
		s.lastKnownStatuses.Store(deviceID, status)
		slog.Info("cleared reboot-required status after observed reboot", "device_id", deviceID, "booted_after", bootedAfter)
	}
}
