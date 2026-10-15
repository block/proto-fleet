package telemetry

import (
	"context"
	"log/slog"
	"time"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

func (s *TelemetryService) reconcileRebootRequiredState(ctx context.Context, deviceID models.DeviceIdentifier, metrics modelsV2.DeviceMetrics, status mm.MinerStatus) {
	bootedAfter, ok := metrics.BootTimeLowerBound(time.Now())
	if !ok {
		return
	}
	if previous, seen := s.lastReconciledBoot.Load(deviceID); seen {
		if previousBound, valid := previous.(time.Time); valid && !bootedAfter.After(previousBound) {
			return
		}
	}
	// Compare the boot evidence to the current reboot boundary in the UPDATE
	// itself. A newer installation must win even if it races this observation.
	updated, err := s.deviceStore.ReconcileRebootRequiredStatus(ctx, deviceID, bootedAfter, status)
	if err != nil {
		slog.Warn("failed to reconcile reboot-required status", "device_id", deviceID, "error", err)
		return // Retry the observation on the next successful telemetry poll.
	}
	if updated {
		// A sample first seen while UPDATING can later satisfy a pending reboot
		// recorded after that boot. Do not cache a query that matched no row.
		s.lastReconciledBoot.Store(deviceID, bootedAfter)
		s.lastKnownStatuses.Store(deviceID, status)
		slog.Info("cleared reboot-required status after observed reboot", "device_id", deviceID, "booted_after", bootedAfter)
	}
}
