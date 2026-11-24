package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"time"

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
// active once targets settle. Curtail failures consume a bounded retry budget; policy
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
	if r.enforceMaxDuration(ctx, ev, targets) || r.enforcePendingDeadline(ctx, ev, targets) {
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

	// Parent-state guards on target writes and the final dispatch boundary
	// close lifecycle races without a separate event read for each target.
	// DISPATCHING includes interrupted writes, aged confirmation attempts,
	// and closed-loop admission claims. Curtail is device-idempotent; the
	// batch helper applies pacing before redispatch.
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
	r.confirmDispatchedTargets(ctx, ev, targets, models.DesiredStateCurtailed)
}

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
	if recordPendingDispatch && !r.recordCurtailPendingDispatch(ctx, ev, r.now()) {
		// Fail closed before either the DISPATCHING pre-write or the physical
		// command. A successful command must never outrun a stale durable clock.
		return false
	}
	// last_dispatched_at is *not* stamped here — only successful enqueues
	// advance it (used by the restore-batch interval gate).
	var dispatchSet []*models.Target
	var claimOK bool
	knownUnsentDeviceIdentifiers := append([]string(nil), preclaimedUnsentDeviceIdentifiers...)
	for _, t := range claim {
		if t.State == models.TargetStatePending {
			knownUnsentDeviceIdentifiers = append(knownUnsentDeviceIdentifiers, t.DeviceIdentifier)
		}
	}
	dispatchSet, claimOK = r.claimDispatchTargets(ctx, ev, claim, models.DesiredStateCurtailed, nonTerminalFailureState)
	if !claimOK {
		return false
	}
	if len(dispatchSet) == 0 {
		return true
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
		result, dispatchErr = r.cmd.Curtail(ctx, selector, sdk.CurtailLevelFull)
	}
	r.applyDispatchResult(ctx, ev, dispatchSet, result, dispatchErr, models.DesiredStateCurtailed, nonTerminalFailureState, curtailment.ToStringSet(knownUnsentDeviceIdentifiers))
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
// terminal at CurtailMaxRetries; restore targets use MaxRetries.
func (r *Reconciler) recordDispatchFailure(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState) {
	r.recordDispatchFailureGuarded(ctx, ev, t, errMsg, nonTerminalFailureState, nil, false)
}

// recordPossiblySentDispatchFailure also records that the failed Curtail may
// have reached the miner, so restoration owes it a compensating Uncurtail.
func (r *Reconciler) recordPossiblySentDispatchFailure(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState) {
	r.recordDispatchFailureGuarded(ctx, ev, t, errMsg, nonTerminalFailureState, nil, true)
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
	}, false)
}

func (r *Reconciler) recordDispatchFailureGuarded(ctx context.Context, ev *models.Event, t *models.Target, errMsg string, nonTerminalFailureState models.TargetState, guard *dispatchFailureGuard, possiblySent bool) {
	newRetry := t.RetryCount + 1
	if t.RetryCount >= r.maxRetriesForTarget(t) {
		newRetry = t.RetryCount
	}
	state := nonTerminalFailureState
	if r.retryBudgetTerminalizes(t, newRetry) {
		state = models.TargetStateRestoreFailed
	}
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:               state,
		LastError:           &errMsg,
		RetryCount:          &newRetry,
		CurtailPossiblySent: possiblySent,
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
		if possiblySent && isCurtailRetryTarget(t) {
			t.CurtailPossiblySent = true
		}
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
	// terminal escalation lands on the next successful UpdateTargetState.
	if bumpErr := r.store.BumpTargetRetry(ctx, ev.ID, t.DeviceIdentifier); bumpErr != nil {
		if !errors.Is(bumpErr, interfaces.ErrCurtailmentEventStateRaceLoss) {
			r.metrics.IncTargetWriteFailure()
			slog.Error("curtailment reconciler: retry-budget bump fallback failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", bumpErr)
		}
		return
	}
	t.RetryCount = newRetry
	if isCurtailRetryTarget(t) {
		t.CurtailPossiblySent = true
	}
}

func (r *Reconciler) maxRetriesForTarget(t *models.Target) int32 {
	if isCurtailRetryTarget(t) {
		return r.cfg.CurtailMaxRetries
	}
	return r.cfg.MaxRetries
}

func (r *Reconciler) retryBudgetTerminalizes(t *models.Target, retryCount int32) bool {
	return t == nil || retryCount >= r.maxRetriesForTarget(t)
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

// enforcePendingDeadline bounds readiness parking as well as dispatch retries.
// Each configured retry gets at least one full tick/confirmation interval.
// The window counts from the latest curtail dispatch or confirmation, plus one
// configured batch interval, so it bounds stalled work rather than cutting off
// a healthy paced plan. Explicitly unbounded events keep waiting until they
// are stopped or aborted. A stalled miner never restores confirmed ones: with
// any miner confirmed, the event gives up on the stalled rest and goes active.
func (r *Reconciler) enforcePendingDeadline(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	if ev.AllowUnbounded {
		return false
	}
	started := ev.CreatedAt
	if started.IsZero() {
		return false
	}
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		for _, progress := range []*time.Time{t.LastDispatchedAt, t.ConfirmedAt, t.CurtailPhase.DispatchedAt} {
			if progress != nil && progress.After(started) {
				started = *progress
			}
		}
	}
	attemptInterval := max(r.cfg.TickInterval, time.Duration(r.cfg.CurtailDispatchTimeoutSec)*time.Second)
	batchInterval := time.Duration(max(ev.CurtailBatchIntervalSec, 0)) * time.Second
	deadline := started.Add(batchInterval + attemptInterval*time.Duration(r.cfg.CurtailMaxRetries))
	if r.now().Before(deadline) {
		return false
	}
	// Retain failed curtail evidence even for targets that never became
	// commandable. Possibly sent failures are reset into the compensating
	// restore queue; known-unsent failures remain terminal without waking hardware.
	reason := "pending curtail readiness deadline exhausted"
	keepCurtailed := hasConfirmedCurtailTarget(targets)
	if store, ok := r.store.(interfaces.CurtailmentPendingDeadlineStore); ok {
		if err := store.FailPendingCurtailTargets(ctx, ev, reason, !keepCurtailed); err != nil {
			r.logEventStateUpdateError(ev, "pending deadline evidence", err)
			return true
		}
	} else {
		return true
	}
	if keepCurtailed {
		now := r.now()
		if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateActive, &now, nil); err != nil {
			r.logEventStateUpdateError(ev, "pending→active(deadline)", err)
		}
		return true
	}
	_, err := r.store.BeginRestoreTransition(ctx, ev.OrgID, ev.EventUUID, interfaces.BeginRestoreTransitionParams{})
	if err != nil {
		r.logEventStateUpdateError(ev, "pending→restoring(deadline)", err)
	}
	return true
}

func hasConfirmedCurtailTarget(targets []*models.Target) bool {
	for _, t := range targets {
		if isCurtailRetryTarget(t) && t.State == models.TargetStateConfirmed {
			return true
		}
	}
	return false
}
