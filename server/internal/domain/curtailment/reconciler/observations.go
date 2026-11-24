package reconciler

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

type observationContextKey struct{}
type targetObservation struct {
	target *models.Target
	patch  interfaces.UpdateCurtailmentTargetStateParams
}
type observationBatch struct{ updates []targetObservation }

func queueObservation(ctx context.Context, target *models.Target, params interfaces.UpdateCurtailmentTargetStateParams) bool {
	batch, ok := ctx.Value(observationContextKey{}).(*observationBatch)
	if !ok {
		return false
	}
	batch.updates = append(batch.updates, targetObservation{target: target, patch: params})
	return true
}

func (r *Reconciler) flushObservations(ctx context.Context, ev *models.Event, batch *observationBatch) {
	if len(batch.updates) == 0 {
		return
	}
	store, ok := r.store.(interfaces.CurtailmentTargetObservationStore)
	if !ok {
		r.metrics.IncTargetWriteFailure()
		slog.Error("curtailment reconciler: observation store is not configured", "event_id", ev.ID)
		return
	}
	observations := make([]interfaces.CurtailmentTargetObservation, 0, len(batch.updates))
	for _, update := range batch.updates {
		observations = append(observations, interfaces.CurtailmentTargetObservation{ExpectedBatchUUID: loadedDispatchBatchUUID(update.target), DeviceIdentifier: update.target.DeviceIdentifier, State: update.patch.State, ObservedPowerW: update.patch.ObservedPowerW, ObservedAt: *update.patch.ObservedAt, ConfirmedAt: update.patch.ConfirmedAt})
	}
	applied, err := store.ObserveTargets(ctx, ev, observations)
	if err != nil {
		r.metrics.IncTargetWriteFailure()
		slog.Error("curtailment reconciler: batch observations failed", "event_id", ev.ID, "error", err)
		return
	}
	appliedSet := curtailment.ToStringSet(applied)
	for _, update := range batch.updates {
		if _, ok := appliedSet[update.target.DeviceIdentifier]; !ok {
			continue
		}
		update.target.State = update.patch.State
		update.target.ObservedAt = update.patch.ObservedAt
		if update.patch.ConfirmedAt != nil {
			update.target.ConfirmedAt = update.patch.ConfirmedAt
		}
		if update.patch.ObservedPowerW != nil {
			update.target.ObservedPowerW = update.patch.ObservedPowerW
		}
	}
}

func (r *Reconciler) dispatchDriftedBatches(ctx context.Context, ev *models.Event, targets []*models.Target) {
	batchSize := int(curtailBatchSizeForEvent(ev, len(targets)))
	for len(targets) > 0 {
		count := min(batchSize, len(targets))
		if !r.dispatchCurtailBatch(ctx, ev, targets[:count], models.TargetStateDrifted, skipPendingDispatchClock) {
			return
		}
		targets = targets[count:]
	}
}
