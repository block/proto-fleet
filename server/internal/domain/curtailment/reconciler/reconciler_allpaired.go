package reconciler

// All-paired policy support for the reconciler. An all-paired FULL_FLEET
// event durably owns every paired-like miner in scope: commandable miners are
// dispatched normally while non-commandable ones are held in the
// TargetStateUnavailable parking state and promoted to pending when they
// become actionable. This file collects the policy-specific admission,
// readiness-refresh, and release logic; the shared closed-loop flow in
// reconciler.go branches into it via isAllPairedPolicyEvent.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func isAllPairedPolicyEvent(ev *models.Event) bool {
	return ev != nil && ev.ForceIncludeAllPairedMiners
}

// isAllPairedPolicyReleasedCurtailTarget identifies dormant policy rows:
// released while the event still wants them curtailed. They hold no dispatch
// work but may be reopened by a later admission pass.
func isAllPairedPolicyReleasedCurtailTarget(ev *models.Event, target *models.Target) bool {
	return isAllPairedPolicyEvent(ev) &&
		target != nil &&
		target.State == models.TargetStateReleased &&
		target.DesiredState == models.DesiredStateCurtailed
}

// claimAllPairedPolicyTargets inserts or reopens durable policy rows for
// every paired-like miner in scope that no event owns yet. Unlike the
// closed-loop dispatch claim, rows enter in their computed policy state
// (pending or unavailable) and dispatch on a later pending pass. The return
// value tells the caller to end the current tick while topology airflow starts
// or while newly admitted rows wait for that later dispatch pass.
func (r *Reconciler) claimAllPairedPolicyTargets(
	ctx context.Context,
	ev *models.Event,
	existingTargets []*models.Target,
	candidates []*models.Candidate,
	params interfaces.ListCandidatesParams,
) bool {
	orgConfig, err := r.store.GetOrgConfig(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: get org config (all-paired admission) failed",
			"event_id", ev.ID, "error", err)
		return false
	}
	activeDevices, err := r.store.ListActiveCurtailmentTargetDevices(ctx, ev.OrgID)
	if err != nil {
		slog.Error("curtailment reconciler: list active devices (all-paired admission) failed",
			"event_id", ev.ID, "error", err)
		return false
	}
	activeSet := toStringSet(activeDevices)
	for _, target := range existingTargets {
		delete(activeSet, target.DeviceIdentifier)
	}
	plan := curtailment.BuildAllPairedPolicyPlan(
		candidates,
		activeSet,
		ev.IncludeMaintenance && ev.ForceIncludeMaintenance,
		candidateMinPowerWForEvent(ev, orgConfig.CandidateMinPowerW),
	)
	targets := curtailment.BuildInsertTargetParams(
		plan.Selected,
		models.ModeFullFleet,
		candidateMinPowerWForEvent(ev, orgConfig.CandidateMinPowerW),
	)
	topologyFilter := isTopologyCandidateFilter(params)
	targets = excludeNonReopenableExistingTargetParams(targets, existingTargets)
	if len(targets) == 0 {
		return false
	}
	if topologyFilter && !r.topologyAdmissionAirflowReady(ctx, ev) {
		return true
	}
	if topologyFilter {
		if curtailBatchIntervalActive(ev) && !r.curtailBatchIntervalElapsed(ev) {
			return false
		}
		if hasInFlightCurtailDispatch(existingTargets) {
			return false
		}
	}
	claimed, err := r.store.ClaimAllPairedPolicyTargets(
		ctx,
		ev.ID,
		ev.OrgID,
		int(curtailBatchSizeForEvent(ev, len(targets))),
		targets,
	)
	if err != nil {
		slog.Error("curtailment reconciler: claim all-paired policy targets failed",
			"event_id", ev.ID, "candidate_count", len(targets),
			"scope_device_count", len(params.DeviceIdentifiers),
			"scope_site_count", len(params.SiteIDs),
			"error", err)
		return false
	}
	if claimed > 0 {
		slog.Info("curtailment reconciler: claimed all-paired policy targets",
			"event_id", ev.ID, "claimed", claimed)
		return topologyFilter
	}
	return false
}

// refreshAllPairedPolicyTargets re-evaluates dispatch readiness for policy
// rows that can still change state (pending/unavailable, desired curtailed):
// promotes unavailable rows whose miner became commandable, demotes pending
// rows whose miner stopped being commandable, and releases rows whose miner
// is no longer paired-like.
//
// Readiness flips are batched into one bulk UPDATE: a mass readiness change
// (fleet-wide outage or recovery) must not issue one round trip per device
// inside the shared tick budget, where it would starve every other event's
// dispatch/drift/restore progress. Releases stay per-row — unpairing is rare.
func (r *Reconciler) refreshAllPairedPolicyTargets(
	ctx context.Context,
	ev *models.Event,
	targets []*models.Target,
	candidates map[string]*models.Candidate,
) {
	minPowerW := candidateMinPowerWForEvent(ev, 0)
	refreshable := make(map[string]*models.Target, len(targets))
	updates := make([]interfaces.AllPairedReadinessUpdate, 0, len(targets))
	for _, target := range targets {
		if target.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if target.State != models.TargetStatePending && target.State != models.TargetStateUnavailable {
			continue
		}
		candidate := candidates[target.DeviceIdentifier]
		if candidate == nil || !curtailment.IsAllPairedPolicyPairingStatus(candidate.PairingStatus) {
			r.releaseAllPairedPolicyTarget(ctx, ev, target, "released: device is no longer paired-like")
			continue
		}
		nextState, reason := curtailment.AllPairedPolicyTargetState(
			candidate,
			ev.IncludeMaintenance && ev.ForceIncludeMaintenance,
		)
		if nextState == target.State && reason == targetErrorString(target) {
			// No readiness flip, but a pending row that promoted while its
			// telemetry was still missing carries no pre-curtail baseline.
			// Keep offering a backfill until one lands — otherwise the
			// promotion tick is the only attempt and confirm/drift checks
			// degrade to the hash-only fallback for the row's lifetime.
			if nextState == models.TargetStatePending && target.BaselinePowerW == nil {
				if baseline := curtailment.AllPairedPromotionBaselinePowerW(candidate, minPowerW); baseline != nil {
					updates = append(updates, interfaces.AllPairedReadinessUpdate{
						DeviceIdentifier:     target.DeviceIdentifier,
						ExpectedState:        target.State,
						ExpectedDesiredState: target.DesiredState,
						State:                nextState,
						Reason:               reason,
						BaselinePowerW:       baseline,
					})
					refreshable[target.DeviceIdentifier] = target
				}
			}
			continue
		}
		if nextState != models.TargetStatePending && nextState != models.TargetStateUnavailable {
			continue
		}
		update := interfaces.AllPairedReadinessUpdate{
			DeviceIdentifier:     target.DeviceIdentifier,
			ExpectedState:        target.State,
			ExpectedDesiredState: target.DesiredState,
			State:                nextState,
			Reason:               reason,
		}
		// Rows inserted while unavailable carry no pre-curtail baseline;
		// backfill it from current telemetry at promotion so confirm/drift
		// checks don't degrade to the hash-only fallback. The SQL never
		// overwrites an existing baseline.
		if nextState == models.TargetStatePending && target.BaselinePowerW == nil {
			update.BaselinePowerW = curtailment.AllPairedPromotionBaselinePowerW(candidate, minPowerW)
		}
		updates = append(updates, update)
		refreshable[target.DeviceIdentifier] = target
	}
	if len(updates) == 0 {
		return
	}
	r.applyAllPairedReadinessUpdates(ctx, ev, refreshable, updates)
}

// refreshAllPairedTopologyRestoreTargets keeps departed policy miners out of
// the restore dispatcher while they cannot receive commands. A restore-failed
// row only leaves its terminal state after first observing an unavailable
// miner; that commandability transition avoids turning ordinary dispatch
// failures into an unbounded retry loop.
func (r *Reconciler) refreshAllPairedTopologyRestoreTargets(
	ctx context.Context,
	ev *models.Event,
	targets []*models.Target,
) bool {
	if !isAllPairedTopologyPolicyEvent(ev) {
		return true
	}

	refreshable := make(map[string]*models.Target, len(targets))
	deviceIdentifiers := make([]string, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.DeviceIdentifier == "" || target.DesiredState != models.DesiredStateActive {
			continue
		}
		switch target.State { //nolint:exhaustive // Only restore states can change commandability.
		case models.TargetStatePending, models.TargetStateUnavailable, models.TargetStateRestoreFailed:
			refreshable[target.DeviceIdentifier] = target
			deviceIdentifiers = append(deviceIdentifiers, target.DeviceIdentifier)
		}
	}
	if len(deviceIdentifiers) == 0 {
		return true
	}

	candidates, err := r.store.ListCandidates(ctx, interfaces.ListCandidatesParams{
		OrgID:             ev.OrgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		slog.Error("curtailment reconciler: list all-paired topology restore candidates failed",
			"event_id", ev.ID, "error", err)
		return false
	}
	candidatesByID := candidatesByDeviceID(candidates)
	updates := make([]interfaces.AllPairedReadinessUpdate, 0, len(refreshable))
	for deviceIdentifier, target := range refreshable {
		nextState, reason := curtailment.AllPairedPolicyRestoreTargetState(candidatesByID[deviceIdentifier])
		if !shouldRefreshAllPairedRestoreTarget(target, nextState, reason) {
			continue
		}
		updates = append(updates, interfaces.AllPairedReadinessUpdate{
			DeviceIdentifier:     deviceIdentifier,
			ExpectedState:        target.State,
			ExpectedDesiredState: target.DesiredState,
			State:                nextState,
			Reason:               reason,
		})
	}
	if len(updates) == 0 {
		return true
	}
	return r.applyAllPairedReadinessUpdates(ctx, ev, refreshable, updates)
}

func shouldRefreshAllPairedRestoreTarget(
	target *models.Target,
	nextState models.TargetState,
	reason string,
) bool {
	switch target.State { //nolint:exhaustive // Callers provide only refreshable restore states.
	case models.TargetStatePending:
		return nextState != models.TargetStatePending
	case models.TargetStateUnavailable:
		return nextState != target.State || reason != targetErrorString(target)
	case models.TargetStateRestoreFailed:
		return nextState != models.TargetStatePending
	default:
		return false
	}
}

func (r *Reconciler) applyAllPairedReadinessUpdates(
	ctx context.Context,
	ev *models.Event,
	refreshable map[string]*models.Target,
	updates []interfaces.AllPairedReadinessUpdate,
) bool {
	appliedDevices, err := r.store.BulkRefreshAllPairedTargetReadiness(ctx, ev.ID, ev.OrgID, ev.State, updates)
	if err != nil {
		r.metrics.IncTargetWriteFailure()
		slog.Error("curtailment reconciler: all-paired readiness bulk refresh failed",
			"event_id", ev.ID, "update_count", len(updates), "error", err)
		return false
	}
	applied := toStringSet(appliedDevices)
	if len(applied) < len(updates) {
		// Rows that advanced concurrently (dispatch claim, Stop reset,
		// event phase change) are skipped by the SQL guards; the next tick
		// re-reads them. Benign, but counted so sustained races surface.
		r.metrics.IncEventStateRaceLoss()
		slog.Warn("curtailment reconciler: all-paired readiness refresh skipped concurrently-advanced rows",
			"event_id", ev.ID, "update_count", len(updates), "applied", len(applied))
	}
	// Mirror only rows the SQL reports as applied: a skipped row's stale
	// in-memory state must not feed the same-tick dispatch pass (a row
	// another actor already advanced could otherwise be flipped back to
	// dispatching and receive a duplicate Curtail).
	for _, update := range updates {
		if _, ok := applied[update.DeviceIdentifier]; !ok {
			continue
		}
		target := refreshable[update.DeviceIdentifier]
		target.State = update.State
		if update.Reason == "" {
			target.LastError = nil
		} else {
			reason := update.Reason
			target.LastError = &reason
		}
		if update.BaselinePowerW != nil && target.BaselinePowerW == nil {
			baseline := *update.BaselinePowerW
			target.BaselinePowerW = &baseline
		}
		if target.DesiredState == models.DesiredStateActive {
			target.ConfirmedAt = nil
			if target.RestorePhase != nil {
				target.RestorePhase.State = update.State
				target.RestorePhase.CompletedAt = nil
				target.RestorePhase.LastError = target.LastError
			}
			if update.State == models.TargetStatePending {
				target.RetryCount = 0
				target.LastDispatchedAt = nil
				target.LastBatchUUID = nil
				if target.RestorePhase != nil {
					target.RestorePhase.RetryCount = 0
					target.RestorePhase.DispatchedAt = nil
					target.RestorePhase.BatchUUID = nil
				}
			}
		}
	}
	return true
}

func (r *Reconciler) releaseAllPairedPolicyTarget(ctx context.Context, ev *models.Event, target *models.Target, reason string) {
	params := interfaces.UpdateCurtailmentTargetStateParams{
		State:     models.TargetStateReleased,
		LastError: &reason,
	}
	if err := r.writeTargetState(ctx, ev, target.DeviceIdentifier, params); err != nil {
		if !errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			slog.Error("curtailment reconciler: all-paired target release failed",
				"event_id", ev.ID, "device", target.DeviceIdentifier, "error", err)
		}
		return
	}
	target.State = models.TargetStateReleased
	target.LastError = &reason
}

func targetErrorString(target *models.Target) string {
	if target == nil || target.LastError == nil {
		return ""
	}
	return *target.LastError
}

// allPairedPolicyRefreshDeviceIdentifiers narrows the readiness-refresh
// candidate query to targets whose state can actually change: dispatched,
// confirmed, released, and restore-phase rows are owned by other passes.
func allPairedPolicyRefreshDeviceIdentifiers(targets []*models.Target) []string {
	if len(targets) == 0 {
		return nil
	}
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.DeviceIdentifier == "" {
			continue
		}
		if target.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if target.State != models.TargetStatePending && target.State != models.TargetStateUnavailable {
			continue
		}
		out = append(out, target.DeviceIdentifier)
	}
	return out
}
