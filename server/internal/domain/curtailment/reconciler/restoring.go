package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"sort"
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
	r.confirmDispatchedTargets(ctx, ev, targets, models.DesiredStateActive)
}

func (r *Reconciler) maybeCompleteRestoring(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	if len(ev.FacilityFanDeviceIDs) > 0 && ev.FanOnSentAt == nil {
		return false
	}
	successful, failed := 0, 0
	for _, t := range targets {
		switch t.State { //nolint:exhaustive // default arm is load-bearing: a future schema-added state must stay non-terminal until it ships its handling, not be silently swept into "complete." TestReconciler_Restoring_UnknownTargetStateKeepsEventNonTerminal pins the contract.
		case models.TargetStateResolved, models.TargetStateReleased:
			if t.CurtailPhase.State == models.TargetStateRestoreFailed {
				failed++
			} else {
				successful++
			}
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
	// Revisit parked obligations: deletion is permanent and must not pin the
	// event in restoring forever. Offline live devices remain parked.
	if isAllPairedTopologyPolicyEvent(ctx, ev) {
		var parked []*models.Target
		var ids []string
		for _, target := range targets {
			if target.DesiredState == models.DesiredStateActive && target.State == models.TargetStateUnavailable {
				parked = append(parked, target)
				ids = append(ids, target.DeviceIdentifier)
			}
		}
		if len(ids) > 0 {
			candidates, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{OrgID: ev.OrgID, DeviceIdentifiers: ids})
			if err != nil {
				return
			}
			byID := candidatesByDeviceID(candidates)
			for _, target := range parked {
				r.parkUncommandableTopologyRestore(ctx, ev, target, byID[target.DeviceIdentifier])
			}
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

	// Prefer targets with fewer failed attempts, so healthy restores make
	// progress before a persistent failure can reuse the next slot.
	targets = append([]*models.Target(nil), targets...)
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].RetryCount < targets[j].RetryCount })

	// DISPATCHING orphans from an interrupted tick may already have an
	// accepted Uncurtail, so they go first. With restore pacing they take this
	// tick's slot on their own: mixing them with fresh PENDING would exceed
	// the batch size and the interval. Re-sending Uncurtail is idempotent.
	claim := claimRestoreTargets(targets, models.TargetStateDispatching, batchSize, batchCapacity)
	if remaining := batchSize - int32(len(claim)); remaining > 0 && (len(claim) == 0 || ev.RestoreBatchSize == 0) { //nolint:gosec // bounded by restore batch size
		claim = append(claim, claimRestoreTargets(targets, models.TargetStatePending, remaining, batchCapacity)...)
	}
	if len(claim) == 0 {
		return
	}

	r.dispatchRestoreBatch(ctx, ev, claim)
}

func claimRestoreTargets(targets []*models.Target, state models.TargetState, batchSize int32, batchCapacity int) []*models.Target {
	claim := make([]*models.Target, 0, batchCapacity)
	for _, t := range targets {
		if t.DesiredState != models.DesiredStateActive || t.State != state {
			continue
		}
		claim = append(claim, t)
		if int32(len(claim)) >= batchSize { //nolint:gosec // bounded by restore batch size
			break
		}
	}
	return claim
}

// dispatchRestoreBatch issues one Uncurtail for every device in the batch
// and per-device commits transitions from the dispatched/skipped split.
// DISPATCHING pre-writes provide the same race-closure as dispatchOneCurtail;
// per-target pre-write failures burn one retry slot via recordDispatchFailure
// so persistent failures escalate to RESTORE_FAILED instead of cycling.
func (r *Reconciler) dispatchRestoreBatch(ctx context.Context, ev *models.Event, claim []*models.Target) {
	dispatchSet, ok := r.claimDispatchTargets(ctx, ev, claim, models.DesiredStateActive, models.TargetStatePending)
	if !ok {
		return
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
	r.applyDispatchResult(ctx, ev, commandTargets, result, dispatchErr, models.DesiredStateActive, models.TargetStatePending, nil)
}

type restoreDispatchFailure struct {
	target       *models.Target
	reason       string
	possiblySent bool
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
	if isAllPairedTopologyPolicyEvent(ctx, ev) {
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
			false,
		)
	}
}

func (r *Reconciler) parkUncommandableTopologyRestore(
	ctx context.Context,
	ev *models.Event,
	target *models.Target,
	candidate *models.Candidate,
) bool {
	if candidate == nil && ev.State == models.EventStateRestoring {
		// ListCandidates includes live unpaired/offline rows; absence means the
		// device was deleted. Preserve the failed restore obligation in history.
		target.RetryCount = r.cfg.MaxRetries - 1
		r.recordDispatchFailure(ctx, ev, target, "restore target device deleted", models.TargetStatePending)
		return true
	}
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
