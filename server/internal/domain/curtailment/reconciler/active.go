package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// observeActive checks drift on confirmed targets and re-dispatches drifted
// targets while OFF remains asserted. ListCandidates over-fetches columns
// the drift check ignores; acceptable at the per-tick fanout scale.
func (r *Reconciler) observeActive(ctx context.Context, ev *models.Event) {
	targets, err := r.store.ListTargetsByEvent(ctx, ev.OrgID, ev.EventUUID)
	if err != nil {
		slog.Error("curtailment reconciler: list targets (active) failed",
			"event_id", ev.ID, "error", err)
		return
	}
	// Deferred confirmation fast-path wake; see dispatchPending.
	defer func() { r.wakeIfDispatchedWork(targets) }()
	if r.enforceMaxDuration(ctx, ev, targets) {
		return
	}
	topologyRestoreInProgress := r.driveTopologyRestores(ctx, ev, targets)
	topologySnapshot, stopTick := r.prepareClosedLoopTopologyTick(ctx, ev, targets)
	if stopTick {
		return
	}

	// State-guarded observations and dispatch claims fence a stale active tick.
	cmdCtx := reconcilerCommandContext(ctx, ev.OrgID, ev.CreatedByUserID)
	var observations *observationBatch
	if _, ok := r.store.(interfaces.CurtailmentTargetObservationStore); ok {
		observations = &observationBatch{}
		cmdCtx = context.WithValue(cmdCtx, observationContextKey{}, observations)
	}
	airflowReopened := false
	deferredDrifted := make([]*models.Target, 0)
	if len(targets) > 0 {
		deviceIDs := make([]string, 0, len(targets))
		for _, t := range targets {
			deviceIDs = append(deviceIDs, t.DeviceIdentifier)
		}
		cands, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
			OrgID:             ev.OrgID,
			DeviceIdentifiers: deviceIDs,
		})
		if err != nil {
			slog.Error("curtailment reconciler: list candidates (drift) failed",
				"event_id", ev.ID, "error", err)
			if ev.FanOffSentAt != nil && len(ev.FacilityFanDeviceIDs) > 0 {
				if !r.reopenEventFans(ctx, ev) {
					return
				}
				airflowReopened = true
			}
		} else {
			candByID := candidatesByDeviceID(cands)
			if isAllPairedPolicyEvent(ev) {
				r.refreshAllPairedPolicyTargets(cmdCtx, ev, targets, candByID)
			}
			for _, t := range targets {
				if !isCurtailRetryTarget(t) {
					continue
				}
				switch t.State {
				case models.TargetStateConfirmed:
					if ev.FanOffSentAt != nil &&
						!hasFreshTelemetry(candByID[t.DeviceIdentifier]) {
						if !airflowReopened {
							if !r.reopenEventFans(ctx, ev) {
								return
							}
							airflowReopened = true
						}
						continue
					}
					r.checkDrift(cmdCtx, ev, t, candByID[t.DeviceIdentifier])
					if t.State == models.TargetStateDrifted && ev.FanOffSentAt != nil {
						deferredDrifted = append(deferredDrifted, t)
					}
				case models.TargetStateDispatched:
					// Re-entry: drifted-then-redispatched, waiting on confirmation.
					r.confirmOneDispatched(cmdCtx, ev, t, candByID[t.DeviceIdentifier], models.TargetStateDispatched)
				case models.TargetStateDispatching:
					// Recovery, timeout retry, or closed-loop admission claim;
					// redispatched after observation in batch-aware order.
					if r.retryBudgetTerminalizes(t, t.RetryCount) {
						// Recover an interrupted write at the retry ceiling.
						r.recordDispatchFailure(cmdCtx, ev, t,
							"retry budget exhausted from interrupted dispatch",
							models.TargetStateDispatching)
						continue
					}
				case models.TargetStateDrifted:
					if r.retryBudgetTerminalizes(t, t.RetryCount) {
						// Recover an interrupted write at the retry ceiling.
						r.recordDispatchFailure(cmdCtx, ev, t,
							"retry budget exhausted on drifted target",
							models.TargetStateDrifted)
						continue
					}
					// Mirror the confirm/drift paired-like guards: a drifted
					// all-paired row whose device unpaired must not receive
					// re-curtail commands every tick.
					{
						if c := candByID[t.DeviceIdentifier]; c == nil {
							r.recordDispatchFailure(cmdCtx, ev, t,
								"candidate row missing (device unpaired or deleted)",
								models.TargetStateDrifted)
							continue
						} else if !curtailment.IsAllPairedPolicyPairingStatus(c.PairingStatus) {
							r.recordDispatchFailure(cmdCtx, ev, t,
								"device is no longer paired-like",
								models.TargetStateDrifted)
							continue
						}
					}
					if ev.FanOffSentAt != nil {
						deferredDrifted = append(deferredDrifted, t)
						continue
					}
					deferredDrifted = append(deferredDrifted, t)
				case models.TargetStatePending, models.TargetStateUnavailable,
					models.TargetStateResolved, models.TargetStateReleased,
					models.TargetStateRestoreFailed:
					// Pending rows are handled by the active closed-loop dispatch pass
					// below. Terminal states are restorer-owned.
				}
			}
			if observations != nil {
				r.flushObservations(cmdCtx, ev, observations)
				// Include newly observed drift only after its guarded batch write.
				deferredDrifted = nil
				for _, target := range targets {
					if target.State == models.TargetStateDrifted && isCurtailRetryTarget(target) && target.RetryCount < r.cfg.CurtailMaxRetries {
						c := candByID[target.DeviceIdentifier]
						if c != nil && curtailment.IsAllPairedPolicyPairingStatus(c.PairingStatus) {
							deferredDrifted = append(deferredDrifted, target)
						}
					}
				}
			}
			if activeFanTargetsNeedAirflow(ev, targets) {
				if !r.reconcileActiveFans(ctx, ev, targets) {
					return
				}
				airflowReopened = true
			}
			r.dispatchDriftedBatches(cmdCtx, ev, deferredDrifted)
			r.dispatchPendingCurtailBatches(cmdCtx, ev, targets)
		}
	}
	if topologyRestoreInProgress {
		if !airflowReopened {
			_ = r.reconcileActiveFans(ctx, ev, targets)
		}
		return
	}
	claimed, deferDispatch := r.claimClosedLoopFullFleetTargetsFromSnapshot(
		ctx,
		ev,
		targets,
		topologySnapshot,
	)
	if deferDispatch {
		return
	}
	if len(claimed) > 0 && !airflowReopened && fanAirflowNeedsReopen(ev) {
		// A closed-loop event may discover a hashing miner after airflow was
		// stopped. Reopen the fans while the new target is still undispatched;
		// later ticks keep them ON until every admitted target confirms curtailment.
		if !r.reconcileActiveFans(ctx, ev, append(targets, claimed...)) {
			return
		}
		airflowReopened = true
	}
	r.dispatchClaimedCurtailTargets(cmdCtx, ev, claimed)
	// Claimed rows aren't in the deferred wake's `targets` slice; wake for
	// them explicitly so dynamically-admitted miners get the pulse too.
	r.wakeIfDispatchedWork(claimed)
	if !airflowReopened {
		_ = r.reconcileActiveFans(ctx, ev, append(targets, claimed...))
	}
}

func (r *Reconciler) driveTopologyRestores(ctx context.Context, ev *models.Event, targets []*models.Target) bool {
	if ev == nil || (ev.State != models.EventStatePending && ev.State != models.EventStateActive) {
		return false
	}
	if !r.refreshAllPairedTopologyRestoreTargets(ctx, ev, targets) {
		return true
	}
	if !hasTopologyRestoreWork(targets) {
		return false
	}
	r.confirmDispatchedRestores(ctx, ev, targets)
	if !hasTopologyRestoreWork(targets) {
		return false
	}
	if len(ev.FacilityFanDeviceIDs) > 0 && fanAirflowNeedsReopen(ev) {
		if !r.reopenEventFans(ctx, ev) {
			return true
		}
	}
	r.maybeClaimRestoreBatch(ctx, ev, targets)
	r.wakeIfDispatchedWork(targets)
	return hasTopologyRestoreWork(targets)
}

func hasTopologyRestoreWork(targets []*models.Target) bool {
	for _, target := range targets {
		if target == nil || target.DesiredState != models.DesiredStateActive {
			continue
		}
		if target.State != models.TargetStateUnavailable && target.State != models.TargetStateResolved && target.State != models.TargetStateRestoreFailed &&
			target.State != models.TargetStateReleased {
			return true
		}
	}
	return false
}

// confirmOneDispatched promotes Dispatched → Confirmed when telemetry
// shows curtailment, resetting retry_count. Missing candidate row goes
// through guarded failure aging so a vanished device can't stall.
func (r *Reconciler) confirmOneDispatched(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate, nonTerminalState models.TargetState) {
	r.confirmTarget(ctx, ev, t, c, false, nonTerminalState)
}

func (r *Reconciler) checkDrift(ctx context.Context, ev *models.Event, t *models.Target, c *models.Candidate) {
	if c == nil {
		r.recordDispatchFailure(ctx, ev, t, "candidate row missing (device unpaired or deleted)", models.TargetStateDrifted)
		return
	}
	if !curtailment.IsAllPairedPolicyPairingStatus(c.PairingStatus) {
		r.recordDispatchFailure(ctx, ev, t, "device is no longer paired-like", models.TargetStateDrifted)
		return
	}
	if !isCurtailed(c.LatestPowerW, t.BaselinePowerW, c.LatestHashRateHS, r.cfg.DriftThresholdFactor, false) {
		now := r.now()
		params := interfaces.UpdateCurtailmentTargetStateParams{
			State:      models.TargetStateDrifted,
			ObservedAt: &now,
		}
		if c.LatestPowerW != nil && curtailment.IsFinite(*c.LatestPowerW) {
			power := *c.LatestPowerW
			params.ObservedPowerW = &power
		}
		if queueObservation(ctx, t, params) {
			return
		}
		if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
			if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
				slog.Error("curtailment reconciler: target drift update failed",
					"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
			}
			return
		}
		t.State = models.TargetStateDrifted
		t.ObservedAt = &now
		if params.ObservedPowerW != nil {
			t.ObservedPowerW = params.ObservedPowerW
		}
		// A target at its retry ceiling is no longer dispatchable.
		if r.retryBudgetTerminalizes(t, t.RetryCount) {
			return
		}
		if ev.FanOffSentAt == nil {
			r.dispatchOneCurtail(ctx, ev, t, models.TargetStateDrifted)
		}
		return
	}
	// Still curtailed: refresh observed_power_w / observed_at as a rolling read.
	now := r.now()
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:      models.TargetStateConfirmed,
		ObservedAt: &now,
	}
	if ev.FanAirflowReopenedAt != nil &&
		(t.ConfirmedAt == nil || t.ConfirmedAt.Before(*ev.FanAirflowReopenedAt)) &&
		hasFreshCurtailedFanEvidence(c, t, r.cfg.DriftThresholdFactor) {
		params.ConfirmedAt = &now
	}
	if c.LatestPowerW != nil && curtailment.IsFinite(*c.LatestPowerW) {
		power := *c.LatestPowerW
		params.ObservedPowerW = &power
	}
	if queueObservation(ctx, t, params) {
		return
	}
	if err := r.writeTargetState(ctx, ev, t.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: target observe update failed",
				"event_id", ev.ID, "device", t.DeviceIdentifier, "error", err)
		}
		return
	}
	t.ObservedAt = &now
	if params.ConfirmedAt != nil {
		t.ConfirmedAt = params.ConfirmedAt
	}
	if params.ObservedPowerW != nil {
		t.ObservedPowerW = params.ObservedPowerW
	}
}

// maybeMarkActive flips Pending → Active once in-flight work has settled,
// while preserving policy readiness holds and topology restore obligations.
// Failed command attempts terminalize at their configured retry ceiling.
func (r *Reconciler) maybeMarkActive(ctx context.Context, ev *models.Event, targets []*models.Target) {
	confirmed, terminalFailures, unavailable, releasedPolicy := 0, 0, 0, 0
	topologyWatcher := isClosedLoopTopologyEvent(ctx, ev)
	if len(targets) == 0 && isAllPairedPolicyEvent(ev) {
		r.warnIfAllPairedPendingStalled(ev, 0, 0)
		return
	}
	for _, t := range targets {
		switch t.State {
		case models.TargetStateConfirmed:
			confirmed++
		case models.TargetStateRestoreFailed:
			if topologyWatcher && t.DesiredState == models.DesiredStateActive {
				// A departed miner exhausted its restore retries. The target keeps
				// the failure visible, but it must not terminate the scope watcher.
				continue
			}
			terminalFailures++
		case models.TargetStateUnavailable:
			if isAllPairedPolicyEvent(ev) {
				// Policy ownership is held until the miner becomes commandable;
				// it should not pause drift enforcement for confirmed targets.
				unavailable++
				continue
			}
			return
		case models.TargetStatePending, models.TargetStateDispatching, models.TargetStateDispatched, models.TargetStateDrifted:
			// In flight.
			return
		case models.TargetStateReleased:
			if isAllPairedPolicyReleasedCurtailTarget(ev, t) {
				// Released all-paired policy rows are dormant placeholders.
				// Admission can reopen them when the miner is paired-like
				// again, so they must not pin the parent event in pending —
				// but with nothing confirmed they must not activate it
				// either (see the hold below).
				releasedPolicy++
				continue
			}
			if topologyWatcher {
				// The miner left the scope before Curtail was dispatched, so there
				// is no restore obligation and the watcher can continue.
				continue
			}
			// Unreachable on a pending event; hold for manual cleanup.
			return
		case models.TargetStateResolved:
			if t.DesiredState != models.DesiredStateActive || !isClosedLoopFullFleet(ev) {
				// Unreachable outside a completed topology restore; hold for manual cleanup.
				return
			}
		}
	}
	if confirmed == 0 && (unavailable > 0 || releasedPolicy > 0) {
		// Keep readiness and admission running while no miner is confirmed.
		// Capped events still end at cap expiry or the readiness deadline;
		// unbounded events wait until Stop or abort.
		r.warnIfAllPairedPendingStalled(ev, unavailable, releasedPolicy)
		return
	}
	if confirmed == 0 && terminalFailures > 0 {
		// A successful or ambiguous send can have physically curtailed a miner
		// even if telemetry never confirms. Restore that obligation before
		// releasing the event; fan intent also requires recovery. Known-unsent
		// failures owe nothing.
		needsRestore := ev.FanOffSentAt != nil
		for _, target := range targets {
			if target.CurtailPhase.DispatchedAt != nil || target.LastDispatchedAt != nil || target.CurtailPossiblySent || target.RestorePhase != nil {
				needsRestore = true
			}
		}
		if needsRestore {
			_, err := r.store.BeginRestoreTransition(ctx, ev.OrgID, ev.EventUUID, interfaces.BeginRestoreTransitionParams{})
			if err != nil {
				r.logEventStateUpdateError(ev, "pending→restoring(give-up)", err)
			}
			return
		}
		// All-failed: nothing curtailed → skip Active.
		now := r.now()
		slog.Warn("curtailment reconciler: pending event has all-terminal targets; marking completed_with_failures",
			"event_id", ev.ID, "failed_target_count", terminalFailures)
		if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateCompletedWithFailures, nil, &now); err != nil {
			r.logEventStateUpdateError(ev, "pending→completed_with_failures", err)
		}
		return
	}
	now := r.now()
	if terminalFailures > 0 {
		slog.Warn("curtailment reconciler: pending→active with terminal-failed targets",
			"event_id", ev.ID, "failed_target_count", terminalFailures, "confirmed_count", confirmed)
	}
	if err := r.store.UpdateEventState(ctx, ev.ID, ev.State, models.EventStateActive, &now, nil); err != nil {
		r.logEventStateUpdateError(ev, "pending→active", err)
	}
}

// allPairedPendingStallWarnAfter is how long an all-paired event may hold in
// pending (started_at unset, nothing confirmed) before the reconciler starts
// flagging it each tick. The event owns its scope, blocking other starts via
// the hierarchy conflict check and scope watcher, until confirmation or Stop;
// a capped event also ends at cap expiry or the pending readiness deadline.
// A sustained stall (fleet-wide outage, auth-needed population) must surface
// on dashboards, not only in the UI.
const allPairedPendingStallWarnAfter = 15 * time.Minute

// warnIfAllPairedPendingStalled emits the stall signal for a held pending
// all-paired event. StartedAt is checked so recurtailed events (restoring →
// pending with started_at already stamped) don't count: their clock ran.
func (r *Reconciler) warnIfAllPairedPendingStalled(ev *models.Event, unavailable, releasedPolicy int) {
	if !isAllPairedPolicyEvent(ev) || ev.StartedAt != nil {
		return
	}
	stalledFor := r.now().Sub(ev.CreatedAt)
	if stalledFor < allPairedPendingStallWarnAfter {
		return
	}
	r.metrics.IncAllPairedPendingStall()
	slog.Warn("curtailment reconciler: all-paired event pending with nothing confirmed; scope stays locked until confirmation or Stop",
		"event_id", ev.ID, "event_uuid", ev.EventUUID,
		"pending_for", stalledFor.Truncate(time.Second).String(),
		"unavailable_targets", unavailable, "released_targets", releasedPolicy)
}
