package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/session"

	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// claimDispatchTargets is the shared, guarded intent phase for Curtail and
// Uncurtail. A Stop wins against stale claims under the event row lock.
func (r *Reconciler) claimDispatchTargets(ctx context.Context, ev *models.Event, claim []*models.Target, desired string, failureState models.TargetState) ([]*models.Target, bool) {
	out := make([]*models.Target, 0, len(claim))
	for _, target := range claim {
		if r.retryBudgetTerminalizes(target, target.RetryCount) {
			r.recordDispatchFailure(ctx, ev, target, "dispatch retry budget exhausted", failureState)
			continue
		}
		expected := target.State
		params := interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateDispatching, ExpectedDesiredState: &desired, ExpectedState: &expected}
		if err := r.writeTargetState(ctx, ev, target.DeviceIdentifier, params); err != nil {
			if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				return nil, false
			}
			r.recordDispatchFailure(ctx, ev, target, err.Error(), failureState)
			continue
		}
		target.State = models.TargetStateDispatching
		out = append(out, target)
	}
	return out, true
}

func (r *Reconciler) confirmDispatchedTargets(ctx context.Context, ev *models.Event, targets []*models.Target, desired string) {
	var ids []string
	for _, target := range targets {
		if target.State == models.TargetStateDispatched && (target.DesiredState == desired || (target.DesiredState == "" && desired == models.DesiredStateCurtailed)) {
			ids = append(ids, target.DeviceIdentifier)
		}
	}
	if len(ids) == 0 {
		return
	}
	candidates, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{OrgID: ev.OrgID, DeviceIdentifiers: ids})
	if err != nil {
		slog.Error("curtailment reconciler: confirmation candidates", "event_id", ev.ID, "error", err)
		return
	}
	byID := candidatesByDeviceID(candidates)
	for _, target := range targets {
		if target.State != models.TargetStateDispatched || (target.DesiredState != desired && !(target.DesiredState == "" && desired == models.DesiredStateCurtailed)) {
			continue
		}
		missingState := models.TargetStateDispatched
		if desired == models.DesiredStateActive {
			missingState = models.TargetStatePending
		}
		r.confirmTarget(ctx, ev, target, byID[target.DeviceIdentifier], desired == models.DesiredStateActive, missingState)
	}
}

// confirmTarget shares observation, timeout, state/batch fencing and mirroring
// between both directions. Only the telemetry predicate and terminal state vary.
func (r *Reconciler) confirmTarget(ctx context.Context, ev *models.Event, target *models.Target, candidate *models.Candidate, restore bool, missingFailureState models.TargetState) {
	if restore && isAllPairedTopologyPolicyEvent(ctx, ev) && r.parkUncommandableTopologyRestore(ctx, ev, target, candidate) {
		return
	}
	if candidate == nil {
		r.recordDispatchedObservationFailure(ctx, ev, target, "candidate row missing (device deleted)", missingFailureState)
		return
	}
	if !restore && !curtailment.IsAllPairedPolicyPairingStatus(candidate.PairingStatus) {
		r.recordDispatchedObservationFailure(ctx, ev, target, "device is no longer paired-like", missingFailureState)
		return
	}
	observed := isCurtailed(candidate.LatestPowerW, target.BaselinePowerW, candidate.LatestHashRateHS, r.cfg.DriftThresholdFactor, true)
	timeoutSec := r.cfg.CurtailDispatchTimeoutSec
	retryState := models.TargetStateDispatching
	if restore {
		observed = isRestored(candidate.LatestPowerW, target.BaselinePowerW, candidate.LatestHashRateHS, r.cfg.DriftThresholdFactor)
		timeoutSec = r.cfg.RestoreDispatchTimeoutSec
		retryState = models.TargetStatePending
	}
	if !observed {
		if target.LastDispatchedAt != nil && timeoutSec > 0 && r.now().Sub(*target.LastDispatchedAt) > time.Duration(timeoutSec)*time.Second {
			reason := "curtail telemetry timeout"
			if restore {
				reason = "restore telemetry timeout"
			}
			r.recordDispatchedObservationFailure(ctx, ev, target, reason, retryState)
		}
		return
	}
	now := r.now()
	params := confirmedCurtailTargetParams(now, candidate.LatestPowerW)
	if restore {
		params = resolvedRestoreTargetParams(now, candidate.LatestPowerW)
	}
	desired := models.DesiredStateCurtailed
	if restore {
		desired = models.DesiredStateActive
	}
	params.ExpectedDesiredState = &desired
	expected := models.TargetStateDispatched
	params.ExpectedState = &expected
	params.ExpectedDispatchBatchUUID = loadedDispatchBatchUUID(target)
	if err := r.writeTargetState(ctx, ev, target.DeviceIdentifier, params); err != nil {
		return
	}
	target.State = params.State
	target.ConfirmedAt = &now
	target.ObservedAt = &now
	if !restore {
		target.RetryCount = 0
	}
	if params.ObservedPowerW != nil {
		target.ObservedPowerW = params.ObservedPowerW
	}
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

// applyDispatchResult is the common enqueue/skip/failure settlement phase.
// Direction-specific restoration parking remains in recordRestoreDispatchFailures.
// knownUnsent names Curtail targets with no earlier send this attempt could
// shadow; a skip of any other target may follow an interrupted send.
func (r *Reconciler) applyDispatchResult(ctx context.Context, ev *models.Event, targets []*models.Target, result *command.CommandResult, dispatchErr error, desired string, failureState models.TargetState, knownUnsent map[string]struct{}) {
	skipped := map[string]string{}
	dispatched := map[string]struct{}{}
	batchID := ""
	if result != nil {
		batchID = result.BatchIdentifier
		dispatched = curtailment.ToStringSet(result.DispatchedDeviceIdentifiers)
		for _, skip := range result.Skipped {
			skipped[skip.DeviceIdentifier] = skippedDeviceReason(skip)
		}
	}
	failures := make([]restoreDispatchFailure, 0, len(targets))
	now := r.now()
	for _, target := range targets {
		reason := skipped[target.DeviceIdentifier]
		switch {
		case dispatchErr != nil:
			reason = dispatchErr.Error()
		case reason != "":
		case batchID == "":
			reason = "command produced no batch (no live devices to dispatch)"
		default:
			if _, ok := dispatched[target.DeviceIdentifier]; !ok {
				reason = "curtail command did not enqueue device"
				if desired == models.DesiredStateActive {
					reason = "uncurtail command did not enqueue device"
				}
			}
		}
		if reason != "" {
			_, unsent := knownUnsent[target.DeviceIdentifier]
			failures = append(failures, restoreDispatchFailure{target: target, reason: reason, possiblySent: dispatchErr != nil || !unsent})
			continue
		}
		emptyError := ""
		params := interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateDispatched, LastDispatchedAt: &now, LastBatchUUID: &batchID, LastError: &emptyError, ExpectedDesiredState: &desired}
		if err := r.writeTargetState(ctx, ev, target.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				// The command was enqueued even though its timestamp was not persisted.
				r.recordPossiblySentDispatchFailure(ctx, ev, target, err.Error(), failureState)
			}
			continue
		}
		target.State = models.TargetStateDispatched
		target.LastDispatchedAt = &now
		target.LastBatchUUID = &batchID
		target.LastError = nil
		phase := &target.CurtailPhase
		if desired == models.DesiredStateActive {
			if target.RestorePhase == nil {
				target.RestorePhase = &models.TargetPhaseSummary{Phase: models.TargetPhaseRestore}
			}
			phase = target.RestorePhase
		}
		phase.State = models.TargetStateDispatched
		phase.DispatchedAt = &now
		phase.BatchUUID = &batchID
	}
	if desired == models.DesiredStateActive {
		r.recordRestoreDispatchFailures(ctx, ev, failures)
	} else {
		for _, failure := range failures {
			if failure.possiblySent {
				r.recordPossiblySentDispatchFailure(ctx, ev, failure.target, failure.reason, failureState)
			} else {
				r.recordDispatchFailure(ctx, ev, failure.target, failure.reason, failureState)
			}
		}
	}
}

// logEventStateUpdateError records benign lifecycle races separately from write failures.
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
