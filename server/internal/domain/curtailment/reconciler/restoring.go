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
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// observeRestoring drives a restoring event toward terminal. Per tick:
// confirm dispatched restores via telemetry; flip terminal when all
// targets are terminal; claim the next batch behind the in-flight +
// interval gates (inrush/thermal-shock protection).
func (r *Reconciler) observeRestoring(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets (restoring) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	// Deferred confirmation fast-path wake; see dispatchPending.
	defer func() { r.wakeIfDispatchedWork(targets) }()
	r.reconcileRestoringFans(ctx, ev)

	if !r.refreshAllPairedTopologyRestoreTargets(ctx, ev, targets) {
		return
	}
	r.confirmDispatchedRestores(ctx, ev, targets)
	if r.maybeCompleteRestoring(ctx, ev, targets) {
		return
	}
	r.maybeClaimRestoreBatch(ctx, ev, targets)
}

// confirmDispatchedRestores promotes restore-phase Dispatched targets to
// Resolved when telemetry shows mining has resumed.
func (r *Reconciler) confirmDispatchedRestores(ctx context.Context, ev *models.Event, targets []*models.Target) {
	deviceIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.DesiredState == models.DesiredStateActive && t.State == models.TargetStateDispatched {
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
		slog.Error("curtailment reconciler: list candidates (restore confirm) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	candByID := candidatesByDeviceID(cands)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive || t.State != models.TargetStateDispatched {
			continue
		}
		r.confirmOneRestore(ctx, ev, t, candByID[t.DeviceIdentifier])
	}
}

// confirmOneRestore promotes Dispatched → Resolved when telemetry shows
// the miner is back above the restore threshold; mirrors
// confirmOneDispatched. Vanished devices normally burn retry budget to keep
// progress; all-paired topology obligations park until commandable instead.
func (r *Reconciler) confirmOneRestore(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate) {
	if isAllPairedTopologyPolicyEvent(ev) && r.parkUncommandableTopologyRestore(ctx, ev, t, c) {
		return
	}
	if c == nil {
		r.recordDispatchedObservationFailure(ctx, ev, t,
			"candidate row missing (device unpaired or deleted)",
			models.TargetStatePending)
		return
	}
	if !isRestored(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor) {
		// Age out targets whose telemetry never confirms so a stale
		// candidate can't pin the event in restoring forever.
		if t.LastDispatchedAt != nil && r.cfg.RestoreDispatchTimeoutSec > 0 {
			timeout := time.Duration(r.cfg.RestoreDispatchTimeoutSec) * time.Second
			if r.now().Sub(*t.LastDispatchedAt) > timeout {
				slog.Info("curtailment reconciler: restore telemetry timeout aging initiated",
					"event_id", ev.ID, "device", t.DeviceIdentifier,
					"timeout_sec", r.cfg.RestoreDispatchTimeoutSec)
				// Guard on dispatched: if the confirmation pulse already
				// resolved this restore target, this stale-snapshot aging write
				// race-loses instead of reverting it — critically avoiding a
				// spurious RESTORE_FAILED when retry budget is at the ceiling.
				r.recordDispatchedObservationFailure(ctx, ev, t,
					"restore telemetry timeout",
					models.TargetStatePending)
			}
		}
		return
	}
	now := r.now()
	params := resolvedRestoreTargetParams(now, c.LatestPowerW)
	desiredActive := models.DesiredStateActive
	params.ExpectedDesiredState = &desiredActive
	expectedTargetState := models.TargetStateDispatched
	params.ExpectedState = &expectedTargetState
	params.ExpectedDispatchBatchUUID = loadedDispatchBatchUUID(t)
	if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: restore confirm update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
		return
	}
	t.State = models.TargetStateResolved
	t.ConfirmedAt = &now
	t.ObservedAt = &now
	if params.ObservedPowerW != nil {
		t.ObservedPowerW = params.ObservedPowerW
	}
}

// maybeCompleteRestoring transitions the event terminal once every target
// is in a terminal state. Returns true when the transition was attempted so
// the caller skips further work this tick.
func (r *Reconciler) maybeCompleteRestoring(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	if len(ev.FacilityFanDeviceIDs) > 0 && ev.FanOnSentAt == nil {
		return false
	}
	successful, failed := 0, 0
	for _, t := range targets {
		switch t.State { //nolint:exhaustive // default arm is load-bearing: a future schema-added state must stay non-terminal until it ships its handling, not be silently swept into "complete." TestReconciler_Restoring_UnknownTargetStateKeepsEventNonTerminal pins the contract.
		case models.TargetStateResolved, models.TargetStateReleased:
			successful++
		case models.TargetStateRestoreFailed:
			failed++
		case models.TargetStatePending, models.TargetStateDispatching,
			models.TargetStateDispatched, models.TargetStateDrifted,
			models.TargetStateConfirmed:
			return false
		default:
			// Unknown future state: don't sweep prematurely.
			return false
		}
	}
	finalState := models.EventStateCompleted
	if failed > 0 {
		finalState = models.EventStateCompletedWithFailures
	}
	now := r.now()
	if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, finalState, nil, &now); err != nil {
		r.logEventStateUpdateError(ev, "restoring→"+string(finalState), err)
		return true
	}
	slog.Info("curtailment reconciler: event terminal",
		"event_id", ev.ID, "event_uuid", ev.EventUUID,
		"final_state", finalState, "successful", successful, "failed", failed)
	return true
}

func restoreClaimBatchSize(ev *models.Event) int32 {
	if ev != nil && ev.RestoreBatchSize == 0 {
		return curtailment.RestoreBatchSizeMax
	}
	return batchSizeForEvent(ev)
}

// maybeClaimRestoreBatch enforces the in-flight + interval gates, then claims
// pending restore targets and dispatches one Uncurtail covering the batch.
func (r *Reconciler) maybeClaimRestoreBatch(ctx context.Context, ev *models.Event, targets []*models.Target) {
	if len(ev.FacilityFanDeviceIDs) > 0 {
		var delayStartedAt *time.Time
		if ev.State == models.EventStateActive || ev.State == models.EventStatePending {
			// If OFF was never attempted, airflow has remained available and no
			// fan-start delay is needed. Otherwise a successful reopen establishes
			// the delay boundary before any miner is restored.
			if ev.FanOffSentAt != nil {
				delayStartedAt = ev.FanAirflowReopenedAt
				if delayStartedAt == nil {
					return
				}
			}
		} else {
			delayStartedAt = ev.FanOnSentAt
			if airflowStartedAt := restoringFanAirflowStartedAt(ev); airflowStartedAt != nil {
				delayStartedAt = airflowStartedAt
			}
			if delayStartedAt == nil {
				return
			}
		}
		if delayStartedAt != nil && r.now().Before(delayStartedAt.Add(time.Duration(ev.FanRestoreDelaySec)*time.Second)) {
			return
		}
	}
	// Gate 1: no in-flight restore batch.
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive {
			continue
		}
		if t.State == models.TargetStateDispatched {
			return
		}
	}

	// Gate 2: inter-batch interval elapsed. 0 = no wait.
	intervalSec := ev.RestoreBatchIntervalSec
	if intervalSec < 0 {
		intervalSec = 0
	}
	if intervalSec > 0 {
		interval := time.Duration(intervalSec) * time.Second
		var newest *time.Time
		for _, t := range targets {
			if t.DesiredState != models.DesiredStateActive {
				continue
			}
			// Terminal targets retain LastDispatchedAt and serve as the spacing reference.
			if t.LastDispatchedAt == nil {
				continue
			}
			if newest == nil || t.LastDispatchedAt.After(*newest) {
				ts := *t.LastDispatchedAt
				newest = &ts
			}
		}
		if newest != nil && r.now().Sub(*newest) < interval {
			return
		}
	}

	batchSize := restoreClaimBatchSize(ev)
	batchCapacity := int(batchSize)
	if batchCapacity > len(targets) {
		batchCapacity = len(targets)
	}

	// First pass: redispatch any DISPATCHING orphans from an interrupted
	// prior tick. Uncurtail is device-idempotent, so re-sending the
	// command is safe. Orphan recovery consumes this tick's batch slot
	// on its own — mixing orphans with fresh PENDING would double the
	// inrush and bypass the one-batch-per-interval throttle.
	orphans := make([]*models.Target, 0, batchCapacity)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive {
			continue
		}
		if t.State != models.TargetStateDispatching {
			continue
		}
		orphans = append(orphans, t)
		if int32(len(orphans)) >= batchSize { //nolint:gosec // batchSize already bounded
			break
		}
	}
	if len(orphans) > 0 {
		r.dispatchRestoreBatch(ctx, ev, orphans)
		return
	}

	// Second pass: claim fresh PENDING up to batchSize.
	claim := make([]*models.Target, 0, batchCapacity)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive {
			continue
		}
		if t.State != models.TargetStatePending {
			continue
		}
		claim = append(claim, t)
		if int32(len(claim)) >= batchSize { //nolint:gosec // batchSize already bounded
			break
		}
	}
	if len(claim) == 0 {
		return
	}

	r.dispatchRestoreBatch(ctx, ev, claim)
}

// dispatchRestoreBatch issues one Uncurtail for every device in the batch
// and per-device commits transitions from the dispatched/skipped split.
// DISPATCHING pre-writes provide the same race-closure as dispatchOneCurtail;
// per-target pre-write failures burn one retry slot via recordDispatchFailure
// so persistent failures escalate to RESTORE_FAILED instead of cycling.
func (r *Reconciler) dispatchRestoreBatch(ctx context.Context, ev *models.Event, claim []*models.Target) {
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}
	dispatchSet := make([]*models.Target, 0, len(claim))
	desiredActive := models.DesiredStateActive
	for _, t := range claim {
		expectedState := t.State
		dispatchingParams := interfaces.UpdateCurtailmentTargetStateParams{
			State:                models.TargetStateDispatching,
			ExpectedDesiredState: &desiredActive,
			ExpectedState:        &expectedState,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, dispatchingParams); err != nil {
			if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				return
			}
			slog.Error("curtailment reconciler: restore dispatching pre-write failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			r.recordDispatchFailure(ctx, ev, t, err.Error(), models.TargetStatePending)
			continue
		}
		t.State = models.TargetStateDispatching
		dispatchSet = append(dispatchSet, t)
	}
	if len(dispatchSet) == 0 {
		return
	}
	if !r.eventStillDispatchable(ctx, ev) {
		return
	}

	cmdCtx := reconcilerCommandContext(ctx, ev.OrgID, ev.CreatedByUserID)
	var result *command.CommandResult
	var dispatchErr error
	runCommand := func(targets []*models.Target) {
		deviceIDs := make([]string, 0, len(targets))
		for _, target := range targets {
			deviceIDs = append(deviceIDs, target.DeviceIdentifier)
		}
		selector := &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonpb.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		}
		result, dispatchErr = r.cmd.Uncurtail(cmdCtx, selector)
	}
	commandTargets := dispatchSet
	topologyHandled, fenceReached, fencedTargets, fenceErr := r.fenceTopologyRestoreDispatch(
		ctx,
		ev,
		dispatchSet,
		runCommand,
	)
	if topologyHandled {
		if !fenceReached {
			if errors.Is(fenceErr, interfaces.ErrCurtailmentEventStateRaceLoss) {
				return
			}
			errMsg := "topology restore dispatch fence failed"
			if fenceErr != nil {
				errMsg = fenceErr.Error()
			}
			slog.Error("curtailment reconciler: topology restore dispatch fence failed",
				"event_id", ev.ID, "error", fenceErr)
			for _, target := range dispatchSet {
				r.recordDispatchFailure(ctx, ev, target, errMsg, models.TargetStatePending)
			}
			return
		}
		commandTargets = fencedTargets
		if fenceErr != nil {
			slog.Error("curtailment reconciler: topology restore fence ended after command callback",
				"event_id", ev.ID, "error", fenceErr)
		}
		if len(commandTargets) == 0 {
			return
		}
	} else {
		runCommand(commandTargets)
	}
	if dispatchErr != nil {
		errMsg := dispatchErr.Error()
		slog.Error("curtailment reconciler: restore batch dispatch failed",
			"event_id", ev.ID, "batch_size", len(commandTargets), "error", dispatchErr)
		r.recordRestoreDispatchFailures(ctx, ev, restoreDispatchFailures(commandTargets, errMsg))
		return
	}

	// Empty BatchIdentifier = no device resolved through the queue.
	if result == nil || result.BatchIdentifier == "" {
		const reason = "uncurtail command produced no batch (no live devices to dispatch)"
		slog.Warn("curtailment reconciler: restore batch produced empty result",
			"event_id", ev.ID, "batch_size", len(commandTargets))
		r.recordRestoreDispatchFailures(ctx, ev, restoreDispatchFailures(commandTargets, reason))
		return
	}

	skippedSet := make(map[string]string, len(result.Skipped))
	for _, s := range result.Skipped {
		skippedSet[s.DeviceIdentifier] = skippedDeviceReason(s)
	}
	dispatchedSet := curtailment.ToStringSet(result.DispatchedDeviceIdentifiers)

	now := r.now()
	batchID := result.BatchIdentifier
	failures := make([]restoreDispatchFailure, 0, len(commandTargets))
	for _, t := range commandTargets {
		if reason, skipped := skippedSet[t.DeviceIdentifier]; skipped {
			slog.Warn("curtailment reconciler: restore device filter-skipped",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "reason", reason)
			failures = append(failures, restoreDispatchFailure{target: t, reason: reason})
			continue
		}
		if _, dispatched := dispatchedSet[t.DeviceIdentifier]; !dispatched {
			const reason = "uncurtail command did not enqueue device"
			slog.Warn("curtailment reconciler: restore device not dispatched",
				"event_id", ev.ID, "device", t.DeviceIdentifier)
			failures = append(failures, restoreDispatchFailure{target: t, reason: reason})
			continue
		}
		emptyErr := ""
		// Symmetric to the Curtail-phase post-cmd write above.
		desiredActive := models.DesiredStateActive
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:                models.TargetStateDispatched,
			LastDispatchedAt:     &now,
			LastBatchUUID:        &batchID,
			LastError:            &emptyErr,
			ExpectedDesiredState: &desiredActive,
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: restore dispatch state update failed",
					"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
				r.recordDispatchFailure(ctx, ev, t, err.Error(), models.TargetStatePending)
			}
			continue
		}
		t.State = models.TargetStateDispatched
		t.LastDispatchedAt = &now
		t.LastBatchUUID = &batchID
		t.LastError = nil
	}
	r.recordRestoreDispatchFailures(ctx, ev, failures)
}

type restoreDispatchFailure struct {
	target *models.Target
	reason string
}

func restoreDispatchFailures(targets []*models.Target, reason string) []restoreDispatchFailure {
	failures := make([]restoreDispatchFailure, 0, len(targets))
	for _, target := range targets {
		failures = append(failures, restoreDispatchFailure{target: target, reason: reason})
	}
	return failures
}

// recordRestoreDispatchFailures parks all-paired topology obligations that
// became uncommandable between the readiness pass and Uncurtail. This records
// the unavailable transition while it is observable, so a later repair can
// safely requeue the obligation without reopening ordinary terminal failures.
func (r *Reconciler) recordRestoreDispatchFailures(
	ctx context.Context,
	ev *models.Event,
	failures []restoreDispatchFailure,
) {
	if len(failures) == 0 {
		return
	}

	parked := make(map[string]struct{}, len(failures))
	if isAllPairedTopologyPolicyEvent(ev) {
		deviceIdentifiers := make([]string, 0, len(failures))
		for _, failure := range failures {
			if failure.target != nil && failure.target.DeviceIdentifier != "" {
				deviceIdentifiers = append(deviceIdentifiers, failure.target.DeviceIdentifier)
			}
		}
		candidates, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
			OrgID:             ev.OrgID,
			DeviceIdentifiers: deviceIdentifiers,
		})
		if err != nil {
			slog.Error("curtailment reconciler: restore failure readiness refresh failed",
				"event_id", ev.ID, "error", err)
		} else {
			candidatesByID := candidatesByDeviceID(candidates)
			for _, failure := range failures {
				if failure.target != nil && r.parkUncommandableTopologyRestore(
					ctx,
					ev,
					failure.target,
					candidatesByID[failure.target.DeviceIdentifier],
				) {
					parked[failure.target.DeviceIdentifier] = struct{}{}
				}
			}
		}
	}

	for _, failure := range failures {
		if failure.target == nil {
			continue
		}
		if _, ok := parked[failure.target.DeviceIdentifier]; ok {
			continue
		}
		r.recordDispatchFailureGuarded(
			ctx,
			ev,
			failure.target,
			failure.reason,
			models.TargetStatePending,
			&dispatchFailureGuard{expectedState: models.TargetStateDispatching},
		)
	}
}

func (r *Reconciler) parkUncommandableTopologyRestore(
	ctx context.Context,
	ev *models.Event,
	target *models.Target,
	candidate *models.Candidate,
) bool {
	nextState, reason := curtailment.AllPairedPolicyRestoreTargetState(candidate)
	if nextState != models.TargetStateUnavailable {
		return false
	}
	desiredActive := models.DesiredStateActive
	expectedState := target.State
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateUnavailable,
		LastError:            &reason,
		ExpectedState:        &expectedState,
		ExpectedDesiredState: &desiredActive,
	}
	if err := r.writeTargetState(ctx, ev, target.DeviceIdentifier, params); err != nil {
		if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			return true
		}
		slog.Error("curtailment reconciler: park uncommandable restore failed",
			"event_id", ev.ID, "device", target.DeviceIdentifier, "error", err)
		return false
	}
	target.State = models.TargetStateUnavailable
	target.LastError = &reason
	if target.RestorePhase != nil {
		target.RestorePhase.State = models.TargetStateUnavailable
		target.RestorePhase.LastError = &reason
	}
	return true
}
