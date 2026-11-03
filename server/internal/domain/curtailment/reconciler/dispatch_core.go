package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// logEventStateUpdateError buckets store.UpdateEventState errors:
// race-loss → Warn + IncEventStateRaceLoss; other errors → Error.
func (r *Reconciler) logEventStateUpdateError(ev *models.Event, transition string, err error) {
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		r.metrics.IncEventStateRaceLoss()
		slog.Warn("curtailment reconciler: event state advanced concurrently; skipping transition",
			"event_id", ev.ID, "event_uuid", ev.EventUUID,
			"loaded_state", ev.State, "transition", transition)
		return
	}
	slog.Error("curtailment reconciler: "+transition+" transition failed",
		"event_id", ev.ID, "error", err)
}

func (r *Reconciler) recordCurtailPendingDispatch(ctx context.Context, ev *models.Event, dispatchedAt time.Time) bool {
	err := r.store.RecordCurtailPendingDispatch(ctx, ev.ID, ev.State, dispatchedAt)
	if err != nil {
		if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			r.metrics.IncEventStateRaceLoss()
			slog.Warn("curtailment reconciler: event state advanced concurrently; skipping pending dispatch clock update",
				"event_id", ev.ID, "event_uuid", ev.EventUUID,
				"loaded_state", ev.State)
			return false
		}
		slog.Error("curtailment reconciler: pending dispatch clock update failed",
			"event_id", ev.ID, "error", err)
		return false
	}

	ts := dispatchedAt
	ev.LastCurtailPendingDispatchAt = &ts
	return true
}

// writeTargetState wraps store.UpdateTargetState so race-loss routes to
// IncEventStateRaceLoss (benign concurrency) and other errors route to
// IncTargetWriteFailure (operator-actionable). Returns the error for
// site-specific logging.
func (r *Reconciler) writeTargetState(ctx context.Context, ev *models.Event, deviceID string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	if params.ExpectedEventState == nil {
		expectedState := ev.State
		params.ExpectedEventState = &expectedState
	}
	if params.ExpectedDesiredState == nil {
		if desired := expectedDesiredStateForEventState(ev.State); desired != "" {
			expectedDesired := desired
			params.ExpectedDesiredState = &expectedDesired
		}
	}
	err := r.store.UpdateTargetState(ctx, ev.ID, deviceID, params)
	if err == nil {
		return nil
	}
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		r.recordTargetStateRaceLoss(ev, deviceID)
		return err
	}
	r.metrics.IncTargetWriteFailure()
	return err
}

func (r *Reconciler) recordTargetStateRaceLoss(ev *models.Event, deviceID string) {
	r.metrics.IncEventStateRaceLoss()
	slog.Warn("curtailment reconciler: target state advanced concurrently; skipping update",
		"event_id", ev.ID, "event_uuid", ev.EventUUID, "device", deviceID)
}

func expectedDesiredStateForEventState(state models.EventState) string {
	switch state {
	case models.EventStatePending, models.EventStateActive:
		return models.DesiredStateCurtailed
	case models.EventStateRestoring:
		return models.DesiredStateActive
	case models.EventStateCompleted, models.EventStateCompletedWithFailures,
		models.EventStateCancelled, models.EventStateFailed:
		return ""
	}
	return ""
}

// reconcilerContext stamps synthetic session.Info on the dispatch ctx.
// Actor=ActorCurtailment lets CurtailmentActiveFilter recognize self-traffic;
// userID (from curtailment_event.created_by_user_id) satisfies
// command_batch_log.created_by's NOT NULL FK to user(id).
func reconcilerContext(parent context.Context, orgID int64, userID int64) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      reconcilerActorName,
		UserID:         userID,
		OrganizationID: orgID,
		ExternalUserID: reconcilerActorName,
		Username:       reconcilerActorName,
		Actor:          session.ActorCurtailment,
	})
}

func reconcilerCommandContext(parent context.Context, orgID int64, userID int64) context.Context {
	return command.WithCommandActivitySuppressed(reconcilerContext(parent, orgID, userID))
}

func batchSizeForEvent(ev *models.Event) int32 {
	batchSize := int32(1)
	if ev != nil && ev.EffectiveBatchSize != nil && *ev.EffectiveBatchSize > 0 {
		batchSize = *ev.EffectiveBatchSize
	}
	return batchSize
}

func curtailBatchSizeForEvent(ev *models.Event, targetCount int) int32 {
	if ev != nil && ev.CurtailBatchSize != nil && *ev.CurtailBatchSize > 0 {
		return *ev.CurtailBatchSize
	}
	if targetCount <= 0 {
		return 1
	}
	if targetCount > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(targetCount) //nolint:gosec // bounded above
}

func curtailBatchIntervalActive(ev *models.Event) bool {
	return ev != nil && ev.CurtailBatchSize != nil && ev.CurtailBatchIntervalSec > 0
}

func hasUnrecordedCurtailDispatch(targets []*models.Target) bool {
	for _, target := range targets {
		if target == nil || target.State != models.TargetStateDispatching {
			continue
		}
		if target.CurtailPhase.DispatchedAt == nil {
			return true
		}
	}
	return false
}

func (r *Reconciler) curtailBatchIntervalElapsed(ev *models.Event) bool {
	if !curtailBatchIntervalActive(ev) {
		return true
	}
	interval := time.Duration(ev.CurtailBatchIntervalSec) * time.Second
	return ev.LastCurtailPendingDispatchAt == nil || r.now().Sub(*ev.LastCurtailPendingDispatchAt) >= interval
}

// isCurtailed decides whether telemetry shows the target is curtailed.
// Power-vs-baseline ranks above hash_rate; missing baseline falls back
// to hash. requirePositiveEvidence=true is the confirm path (no sample
// → not curtailed); false is the drift path (missing sample preserves
// curtailed so a flaky sensor doesn't restorm).
func isCurtailed(latestPowerW *float64, baselinePowerW *float64, latestHashRateHS *float64, driftThresholdFactor float64, requirePositiveEvidence bool) bool {
	if latestPowerW == nil || !curtailment.IsFinite(*latestPowerW) {
		if requirePositiveEvidence {
			return false
		}
		// Drift path: zero/missing hash → still curtailed; positive hash → resumed.
		if latestHashRateHS == nil || !curtailment.IsFinite(*latestHashRateHS) {
			return true
		}
		return *latestHashRateHS <= 0
	}
	if baselinePowerW != nil && curtailment.IsFinite(*baselinePowerW) && *baselinePowerW > 0 {
		threshold := *baselinePowerW * driftThresholdFactor
		return *latestPowerW <= threshold
	}
	// Baseline missing: hash-only fallback.
	if latestHashRateHS == nil || !curtailment.IsFinite(*latestHashRateHS) {
		return !requirePositiveEvidence
	}
	return *latestHashRateHS <= 0
}

func (r *Reconciler) eventStillDispatchable(ctx context.Context, ev *models.Event) bool {
	latest, err := r.store.GetEventByUUID(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: event liveness check failed",
			"event_id", ev.ID, "event_uuid", ev.EventUUID, "error", err)
		return false
	}
	if latest.State.IsTerminal() || latest.State != ev.State {
		slog.Info("curtailment reconciler: skipping dispatch after event state changed",
			"event_id", ev.ID, "event_uuid", ev.EventUUID,
			"loaded_state", ev.State, "current_state", latest.State)
		return false
	}
	return true
}

// isRestored returns true when telemetry shows the target has resumed
// mining. Requires positive evidence — missing samples return false so
// a flaky sensor doesn't trigger a premature Resolved. The strict > vs
// isCurtailed's <= leaves a no-progress band at exactly baseline×factor.
func isRestored(latestPowerW *float64, baselinePowerW *float64, latestHashRateHS *float64, restoreThresholdFactor float64) bool {
	if latestPowerW != nil && curtailment.IsFinite(*latestPowerW) {
		if baselinePowerW != nil && curtailment.IsFinite(*baselinePowerW) && *baselinePowerW > 0 {
			return *latestPowerW > *baselinePowerW*restoreThresholdFactor
		}
	}
	// Baseline missing / power stale: hash-only fallback.
	if latestHashRateHS == nil || !curtailment.IsFinite(*latestHashRateHS) {
		return false
	}
	return *latestHashRateHS > 0
}
