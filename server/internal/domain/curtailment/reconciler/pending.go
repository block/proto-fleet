package reconciler

import (
	"context"
	"errors"
	"log/slog"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// dispatchPending dispatches Curtail per pending target, confirms
// already-dispatched targets via telemetry, then flips the event to
// active once targets settle. Curtail failures remain retryable; policy
// readiness and topology departure restores also affect activation.
func (r *Reconciler) dispatchPending(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets failed",
			"event_id", ev.ID, "error", err)
		return
	}
	// Enforce the finite safety cap before any fan or miner dispatch, including
	// partially confirmed pending events. Failed transitions still stop dispatch.
	if r.enforceMaxDuration(ctx, ev, targets) {
		return
	}
	// Deferred so both fresh dispatches from this pass and rows already
	// dispatched (recovery) wake the confirmation fast path.
	defer func() { r.wakeIfDispatchedWork(targets) }()
	if !r.reconcilePendingFans(ctx, ev) {
		return
	}
	topologyRestoreInProgress := r.driveTopologyRestores(ctx, ev, targets)
	topologySnapshot, stopTick := r.prepareClosedLoopTopologyTick(ctx, ev, targets)
	if stopTick || topologyRestoreInProgress {
		return
	}
	if len(targets) == 0 && !isAllPairedPolicyEvent(ev) {
		if isClosedLoopFullFleet(ev) {
			now := r.now()
			if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateActive, &now, nil); err != nil {
				r.logEventStateUpdateError(ev, "pending→active(empty closed-loop)", err)
			}
			return
		}
		// Service.Start rejects empty open-loop plans; zero targets is a
		// contract violation needing manual recovery.
		slog.Error("curtailment reconciler: pending event has no targets",
			"event_id", ev.ID, "event_uuid", ev.EventUUID)
		return
	}

	// Liveness check; per-target race-closure happens in dispatchCurtailBatch.
	// DISPATCHING includes interrupted writes, aged confirmation attempts,
	// and closed-loop admission claims. Curtail is device-idempotent; the
	// batch helper applies pacing before redispatch.
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}
	cmdCtx := reconcilerCommandContext(ctx, ev.OrgID, ev.CreatedByUserID)
	if isAllPairedPolicyEvent(ev) {
		deviceIDs := allPairedPolicyRefreshDeviceIdentifiers(targets)
		if len(deviceIDs) > 0 {
			cands, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
				OrgID:             ev.OrgID,
				DeviceIdentifiers: deviceIDs,
			})
			if err != nil {
				slog.Error("curtailment reconciler: list candidates (all-paired pending refresh) failed",
					"event_id", ev.ID, "error", err)
			} else {
				r.refreshAllPairedPolicyTargets(cmdCtx, ev, targets, candidatesByDeviceID(cands))
			}
		}
	}
	r.dispatchPendingCurtailBatches(cmdCtx, ev, targets)

	// Confirm just-dispatched targets via current telemetry before deciding
	// whether the event itself can flip to active.
	r.confirmDispatched(ctx, ev, targets)
	r.maybeMarkActive(ctx, ev, targets)

	// Durable ownership must not pause while the event is pending: recurtail
	// (restoring -> pending) leaves released policy rows dormant for the
	// multi-tick re-confirmation window unless admission also runs here.
	// Claimed/reopened rows enter as pending/unavailable and dispatch on the
	// next tick's pending pass, mirroring the observeActive claim ordering.
	if topologySnapshot != nil || isAllPairedPolicyEvent(ev) {
		claimed, deferDispatch := r.claimClosedLoopFullFleetTargetsFromSnapshot(
			ctx,
			ev,
			targets,
			topologySnapshot,
		)
		if deferDispatch {
			return
		}
		r.dispatchClaimedCurtailTargets(cmdCtx, ev, claimed)
		// Claimed rows are a separate slice the deferred wakeIfDispatchedWork
		// (which only sees `targets`) never covers, so a dynamically-admitted
		// miner would miss the pulse when it is parked. Wake for them too.
		r.wakeIfDispatchedWork(claimed)
	}
}

func (r *Reconciler) reconcilePendingFans(ctx context.Context, ev *models.Event) bool {
	if ev == nil || ev.FanOffSentAt == nil || ev.FanLastError == nil {
		return true
	}
	if r.fans == nil || r.fanStore == nil || len(ev.FacilityFanDeviceIDs) == 0 {
		return false
	}
	now := r.now()
	params := interfaces.UpdateCurtailmentFanStateParams{
		ExpectedEventState: models.EventStatePending,
	}
	if ev.FanAirflowReopenedAt == nil {
		params.FanAirflowReopenedAt = &now
	}
	params.FanAirflowReopenedAtOnSuccess = &now
	lastError, err := r.commandAndPersistFanState(ctx, ev, params, driver.PowerOn)
	if err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: pending facility fan ON failed", "event_id", ev.ID, "error", err)
		}
		return false
	}
	if lastError == nil {
		ev.FanAirflowReopenedAt = params.FanAirflowReopenedAtOnSuccess
	} else if params.FanAirflowReopenedAt != nil {
		ev.FanAirflowReopenedAt = params.FanAirflowReopenedAt
	}
	ev.FanLastError = lastError
	if lastError == nil && r.fanAlert != nil {
		r.fanAlert.EmitCurtailmentFanRestoreFailure(ctx, ev.OrgID, ev.EventUUID.String(), false)
	}
	return lastError == nil
}

// dispatchPendingCurtailBatches drains retryable Curtail work in command
// batches. DISPATCHING recovery and retry rows precede fresh PENDING rows.
// curtail_batch_size=NULL dispatches all remaining targets; a positive
// interval paces fresh pending batches.
func (r *Reconciler) dispatchPendingCurtailBatches(ctx context.Context, ev *models.Event, targets []*models.Target) {
	batchSize := curtailBatchSizeForEvent(ev, len(targets))
	dispatchByState := func(state models.TargetState, dispatchSingleBatch bool, recordPendingDispatch bool) bool {
		dispatchClaim := func(claim []*models.Target) bool {
			recordClaimDispatch := recordPendingDispatch
			if state == models.TargetStateDispatching {
				recordClaimDispatch = recordPendingDispatch && hasUnrecordedCurtailDispatch(claim)
			}
			return r.dispatchCurtailBatch(ctx, ev, claim, state, recordClaimDispatch)
		}
		claim := make([]*models.Target, 0, batchSize)
		for _, t := range targets {
			if t.State != state || !isCurtailRetryTarget(t) {
				continue
			}
			claim = append(claim, t)
			if int32(len(claim)) >= batchSize { //nolint:gosec // batchSize already bounded
				if !dispatchClaim(claim) {
					return false
				}
				if dispatchSingleBatch {
					return true
				}
				claim = make([]*models.Target, 0, batchSize)
			}
		}
		if len(claim) == 0 {
			return true
		}
		return dispatchClaim(claim)
	}

	intervalActive := curtailBatchIntervalActive(ev)
	// A DISPATCHING row without a durable curtail-phase result may have been
	// stranded before its command was enqueued. Treat that recovery as a fresh
	// wave so its physical send is paced. Rows with a durable prior enqueue are
	// retries and deliberately leave the pending-wave clock alone.
	if !dispatchByState(models.TargetStateDispatching, intervalActive, intervalActive) {
		return
	}
	if intervalActive && !r.curtailBatchIntervalElapsed(ev) {
		return
	}
	_ = dispatchByState(models.TargetStatePending, intervalActive, recordPendingDispatchClock)
}

// confirmDispatched promotes Dispatched → Confirmed when telemetry
// shows the device is curtailed.
func (r *Reconciler) confirmDispatched(ctx context.Context, ev *models.Event, targets []*models.Target) {
	deviceIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.State == models.TargetStateDispatched {
			deviceIDs = append(deviceIDs, t.DeviceIdentifier)
		}
	}
	if len(deviceIDs) == 0 {
		return
	}
	cands, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
		OrgID:             ev.OrgID,
		DeviceIdentifiers: deviceIDs,
	})
	if err != nil {
		slog.Error("curtailment reconciler: list candidates (confirm) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	candByID := candidatesByDeviceID(cands)
	for _, t := range targets {
		if t.State != models.TargetStateDispatched {
			continue
		}
		r.confirmOneDispatched(ctx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
	}
}

// dispatchOneCurtail issues one Curtail and records the outcome.
// nonTerminalFailureState is where the target lands on a non-terminal
// failure (Pending or Drifted, per caller).
//
// Race-closure: the DISPATCHING pre-write makes a concurrent
// AdminTerminate see an in-flight target and reject as Stop-first; its
// locked_event FOR UPDATE CTE serializes the target write with parent
// transitions and rejects a parent that left the expected phase.
// Restart-safety: a crash between pre-write and command leaves the
// target in DISPATCHING; the next tick redispatches via
// nonTerminalFailureState (Curtail is device-idempotent).
func (r *Reconciler) dispatchOneCurtail(ctx context.Context, ev *models.Event, t *models.Target, nonTerminalFailureState models.TargetState) {
	_ = r.dispatchCurtailBatch(ctx, ev, []*models.Target{t}, nonTerminalFailureState, skipPendingDispatchClock)
}

// dispatchCurtailBatch issues one Curtail command for every device in claim and
// records per-target dispatched/skipped/failed outcomes.
func (r *Reconciler) dispatchCurtailBatch(ctx context.Context, ev *models.Event, claim []*models.Target, nonTerminalFailureState models.TargetState, recordPendingDispatch bool) bool {
	return r.dispatchCurtailBatchWithKnownUnsent(
		ctx,
		ev,
		claim,
		nonTerminalFailureState,
		recordPendingDispatch,
		nil,
	)
}

func (r *Reconciler) dispatchCurtailBatchWithKnownUnsent(
	ctx context.Context,
	ev *models.Event,
	claim []*models.Target,
	nonTerminalFailureState models.TargetState,
	recordPendingDispatch bool,
	preclaimedUnsentDeviceIdentifiers []string,
) bool {
	if len(claim) == 0 {
		return true
	}
	if !r.eventStillDispatchable(ctx, ev) {
		return false
	}
	if recordPendingDispatch && !r.recordCurtailPendingDispatch(ctx, ev, r.now()) {
		// Fail closed before either the DISPATCHING pre-write or the physical
		// command. A successful command must never outrun a stale durable clock.
		return false
	}
	// last_dispatched_at is *not* stamped here — only successful enqueues
	// advance it (used by the restore-batch interval gate).
	dispatchSet := make([]*models.Target, 0, len(claim))
	knownUnsentDeviceIdentifiers := append([]string(nil), preclaimedUnsentDeviceIdentifiers...)
	for _, t := range claim {
		if t.State == models.TargetStatePending {
			knownUnsentDeviceIdentifiers = append(knownUnsentDeviceIdentifiers, t.DeviceIdentifier)
		}
		dispatchingParams := interfaces.UpdateCurtailmentTargetStateParams{
			State: models.TargetStateDispatching,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, dispatchingParams); err != nil {
			if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				return false
			}
			slog.Error("curtailment reconciler: dispatching pre-write failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			// Symmetric to dispatchRestoreBatch: burn one retry slot so a
			// row-specific persistent write failure remains observable in retry_count.
			// Curtail targets stay retryable while OFF is asserted.
			r.recordDispatchFailure(ctx, ev, t, err.Error(), nonTerminalFailureState)
			continue
		}
		t.State = models.TargetStateDispatching
		dispatchSet = append(dispatchSet, t)
	}
	if len(dispatchSet) == 0 {
		return true
	}
	if !r.eventStillDispatchable(ctx, ev) {
		return false
	}
	deviceIDs := make([]string, 0, len(dispatchSet))
	for _, t := range dispatchSet {
		deviceIDs = append(deviceIDs, t.DeviceIdentifier)
	}
	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: deviceIDs,
			},
		},
	}
	var result *command.CommandResult
	var dispatchErr error
	topologyHandled, topologyAllowed := r.authorizeTopologyCurtailDispatch(
		ctx,
		ev,
		dispatchSet,
		knownUnsentDeviceIdentifiers,
		func() { result, dispatchErr = r.cmd.Curtail(ctx, selector, sdk.CurtailLevelFull) },
	)
	if topologyHandled {
		if !topologyAllowed {
			return false
		}
	} else {
		if !r.eventStillDispatchable(ctx, ev) {
			return false
		}
		result, dispatchErr = r.cmd.Curtail(ctx, selector, sdk.CurtailLevelFull)
	}
	if dispatchErr != nil {
		errMsg := dispatchErr.Error()
		slog.Error("curtailment reconciler: curtail batch dispatch failed",
			"event_id", ev.ID, "batch_size", len(dispatchSet), "error", dispatchErr)
		for _, t := range dispatchSet {
			r.recordDispatchFailure(ctx, ev, t, errMsg, nonTerminalFailureState)
		}
		return true
	}

	skippedSet := make(map[string]string)
	if result != nil {
		skippedSet = make(map[string]string, len(result.Skipped))
		for _, s := range result.Skipped {
			skippedSet[s.DeviceIdentifier] = skippedDeviceReason(s)
		}
	}
	if result == nil || result.BatchIdentifier == "" {
		const reason = "command produced no batch (no live devices to dispatch)"
		slog.Warn("curtailment reconciler: curtail batch produced empty result",
			"event_id", ev.ID, "batch_size", len(dispatchSet))
		for _, t := range dispatchSet {
			if skipReason, skipped := skippedSet[t.DeviceIdentifier]; skipped {
				r.recordDispatchFailure(ctx, ev, t, skipReason, nonTerminalFailureState)
				continue
			}
			r.recordDispatchFailure(ctx, ev, t, reason, nonTerminalFailureState)
		}
		return true
	}
	dispatchedSet := curtailment.ToStringSet(result.DispatchedDeviceIdentifiers)

	now := r.now()
	emptyErr := ""
	batchID := result.BatchIdentifier
	desiredCurtailed := models.DesiredStateCurtailed
	for _, t := range dispatchSet {
		if skipReason, skipped := skippedSet[t.DeviceIdentifier]; skipped {
			slog.Warn("curtailment reconciler: dispatch filter-skipped",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "reason", skipReason)
			r.recordDispatchFailure(ctx, ev, t, skipReason, nonTerminalFailureState)
			continue
		}
		if _, dispatched := dispatchedSet[t.DeviceIdentifier]; !dispatched {
			const reason = "curtail command did not enqueue device"
			slog.Warn("curtailment reconciler: curtail device not dispatched",
				"event_id", ev.ID, "device", t.DeviceIdentifier)
			r.recordDispatchFailure(ctx, ev, t, reason, nonTerminalFailureState)
			continue
		}
		// Explicit dispatch direction at the call site; writeTargetState's
		// auto-fill would derive the same value from ev.State.
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:                models.TargetStateDispatched,
			LastDispatchedAt:     &now,
			LastError:            &emptyErr,
			LastBatchUUID:        &batchID,
			ExpectedDesiredState: &desiredCurtailed,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: target dispatch update failed",
					"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
				r.recordDispatchFailure(ctx, ev, t, err.Error(), nonTerminalFailureState)
			}
			continue
		}
		// Mirror to the in-memory row for this tick's downstream phases.
		t.State = models.TargetStateDispatched
		t.LastDispatchedAt = &now
		t.LastError = nil
		t.LastBatchUUID = &batchID
		t.CurtailPhase.State = models.TargetStateDispatched
		t.CurtailPhase.DispatchedAt = &now
		t.CurtailPhase.BatchUUID = &batchID
	}
	return true
}

type dispatchFailureGuard struct {
	expectedState     models.TargetState
	expectedBatchUUID *string
}

func loadedDispatchBatchUUID(t *models.Target) *string {
	if t.DesiredState == models.DesiredStateActive {
		if t.RestorePhase == nil {
			return nil
		}
		return t.RestorePhase.BatchUUID
	}
	return t.CurtailPhase.BatchUUID
}

// recordDispatchFailure bumps retry_count. Restore targets transition to
// RestoreFailed at MaxRetries so the event can complete; curtail targets stay
// retryable while OFF remains asserted, with retry_count surfacing the alert.
func (r *Reconciler) recordDispatchFailure(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState) {
	r.recordDispatchFailureGuarded(ctx, ev, t, errMsg, nonTerminalFailureState, nil)
}

// recordDispatchedObservationFailure guards failure writes derived from a
// loaded dispatched target on both its state and dispatch batch. The batch
// token closes the ABA window where another fleetd confirms and redispatches
// the row before this stale snapshot writes. Legacy dispatched rows without a
// batch token retain the state guard so they can still age normally.
func (r *Reconciler) recordDispatchedObservationFailure(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState) {
	r.recordDispatchFailureGuarded(ctx, ev, t, errMsg, nonTerminalFailureState, &dispatchFailureGuard{
		expectedState:     models.TargetStateDispatched,
		expectedBatchUUID: loadedDispatchBatchUUID(t),
	})
}

func (r *Reconciler) recordDispatchFailureGuarded(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState, guard *dispatchFailureGuard) {
	newRetry := t.RetryCount + 1
	state := nonTerminalFailureState
	if r.retryBudgetTerminalizes(t, newRetry) {
		state = models.TargetStateRestoreFailed
	}
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:      state,
		LastError:  &errMsg,
		RetryCount: &newRetry,
	}
	if t.DesiredState != "" {
		expectedDesiredState := t.DesiredState
		params.ExpectedDesiredState = &expectedDesiredState
	}
	if guard != nil {
		params.ExpectedState = &guard.expectedState
		params.ExpectedDispatchBatchUUID = guard.expectedBatchUUID
	}
	err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params)
	if err == nil {
		t.State = state
		t.RetryCount = newRetry
		t.LastError = &errMsg
		return
	}
	if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
		return
	}
	slog.Error("curtailment reconciler: target update after dispatch failure failed",
		"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
	if guard != nil {
		// BumpTargetRetry cannot carry the dispatched-state and phase-batch
		// guards. Falling back here could consume retry budget on a target the
		// pulse already confirmed or on a replacement dispatch batch.
		return
	}
	// Fallback: advance retry budget only. State stays at the prior value;
	// terminal restore escalation lands on the next successful UpdateTargetState.
	if bumpErr := r.store.BumpTargetRetry(ctx, ev.ID, t.DeviceIdentifier); bumpErr != nil {
		if !errors.Is(bumpErr, interfaces.ErrCurtailmentEventStateRaceLoss) {
			r.metrics.IncTargetWriteFailure()
			slog.Error("curtailment reconciler: retry-budget bump fallback failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", bumpErr)
		}
		return
	}
	t.RetryCount = newRetry
}

func (r *Reconciler) maxRetriesForTarget(t *models.Target) int32 {
	if isCurtailRetryTarget(t) {
		return r.cfg.CurtailMaxRetries
	}
	return r.cfg.MaxRetries
}

func (r *Reconciler) retryBudgetTerminalizes(t *models.Target, retryCount int32) bool {
	return t == nil || (!isCurtailRetryTarget(t) && retryCount >= r.maxRetriesForTarget(t))
}

func isCurtailRetryTarget(t *models.Target) bool {
	return t != nil && (t.DesiredState == "" || t.DesiredState == models.DesiredStateCurtailed)
}

// candidatesByDeviceID indexes a candidate slice by device identifier for
// the per-tick observe loops that join targets against telemetry.
func candidatesByDeviceID(cands []*models.Candidate) map[string]*models.Candidate {
	out := make(map[string]*models.Candidate, len(cands))
	for _, c := range cands {
		out[c.DeviceIdentifier] = c
	}
	return out
}

// skippedDeviceReason renders the priority-ordered reason string for a
// filter-skipped device: explicit reason first, filter name next, generic
// fallback last. Shared by the single-device and batch dispatch paths so
// both produce the same audit string.
func skippedDeviceReason(s command.SkippedDevice) string {
	switch {
	case s.Reason != "":
		return s.Reason
	case s.FilterName != "":
		return "filtered by " + s.FilterName
	default:
		return "filtered by command preflight"
	}
}
